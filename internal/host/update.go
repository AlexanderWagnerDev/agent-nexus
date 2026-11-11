package host

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultUpdateRepo = "AlexanderWagnerDev/agent-nexus"

// ErrNoRelease means the release feed is reachable but contains no publishable
// release. During alpha this is the normal state, not an update failure.
var ErrNoRelease = errors.New("no published release")

type UpdateInfo struct {
	Current      string    `json:"current"`
	Latest       string    `json:"latest"`
	Available    bool      `json:"available"`
	Prerelease   bool      `json:"prerelease"`
	AssetName    string    `json:"asset_name,omitempty"`
	ChecksumURL  string    `json:"checksum_url,omitempty"`
	SignatureURL string    `json:"signature_url,omitempty"`
	DownloadURL  string    `json:"download_url,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	PublishedAt  time.Time `json:"published_at,omitempty"`
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func updateAPIBase() string {
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("NEXUS_UPDATE_API")), "/"); value != "" {
		return value
	}
	return "https://api.github.com"
}

func updateRepo() string {
	if value := strings.TrimSpace(os.Getenv("NEXUS_UPDATE_REPO")); value != "" {
		return value
	}
	return defaultUpdateRepo
}

func githubToken() string {
	for _, name := range []string{"NEXUS_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// CheckForUpdate reads the release list directly from GitHub. The hub is never
// involved in choosing or serving the binary, so a compromised hub cannot
// substitute an executable.
func CheckForUpdate(ctx context.Context, currentVersion, channel string) (UpdateInfo, error) {
	info := UpdateInfo{Current: currentVersion}
	if channel == "" || channel == "auto" {
		if strings.Contains(currentVersion, "-") {
			channel = "prerelease"
		} else {
			channel = "stable"
		}
	}
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", updateAPIBase(), updateRepo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AgentNexus-Updater")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return info, errors.New("release repository not found; a private repository needs NEXUS_GITHUB_TOKEN with release read access")
	}
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("release check failed: HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return info, err
	}
	var newest *githubRelease
	var newestStable *githubRelease
	for i := range releases {
		release := &releases[i]
		if release.Draft || release.TagName == "" {
			continue
		}
		version := strings.TrimPrefix(release.TagName, "v")
		if newest == nil || compareVersions(version, strings.TrimPrefix(newest.TagName, "v")) > 0 {
			newest = release
		}
		if !release.Prerelease && (newestStable == nil || compareVersions(version, strings.TrimPrefix(newestStable.TagName, "v")) > 0) {
			newestStable = release
		}
	}
	// A repository that currently publishes only prereleases should still be
	// discoverable; the reported Prerelease flag makes that explicit.
	if channel == "stable" && newestStable != nil {
		newest = newestStable
	}
	if newest == nil {
		return info, ErrNoRelease
	}
	version := strings.TrimPrefix(newest.TagName, "v")
	info.Latest = version
	info.Prerelease = newest.Prerelease
	info.Notes = newest.Body
	info.PublishedAt = newest.PublishedAt
	info.Available = compareVersions(version, currentVersion) > 0
	name := releaseAssetName(version, runtime.GOOS, runtime.GOARCH)
	for _, asset := range newest.Assets {
		switch asset.Name {
		case name:
			info.AssetName, info.DownloadURL = asset.Name, asset.URL
		case "checksums.txt":
			info.ChecksumURL = asset.URL
		case "checksums.txt.sigstore.json":
			info.SignatureURL = asset.URL
		}
	}
	if info.DownloadURL == "" {
		return info, fmt.Errorf("release %s has no asset %s", version, name)
	}
	return info, nil
}

func releaseAssetName(version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("agent-nexus_%s_%s_%s.%s", version, goos, goarch, ext)
}

type semver struct {
	major, minor, patch int
	pre                 []string
}

func parseVersion(value string) semver {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if i := strings.IndexByte(value, '+'); i >= 0 {
		value = value[:i]
	}
	var out semver
	if i := strings.IndexByte(value, '-'); i >= 0 {
		out.pre = strings.Split(value[i+1:], ".")
		value = value[:i]
	}
	parts := strings.Split(value, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		out.parts(i, parts[i])
	}
	return out
}

func (s *semver) parts(index int, raw string) {
	n, _ := strconv.Atoi(raw)
	switch index {
	case 0:
		s.major = n
	case 1:
		s.minor = n
	case 2:
		s.patch = n
	}
}

func compareVersions(a, b string) int {
	left, right := parseVersion(a), parseVersion(b)
	if left.major != right.major {
		return intCmp(left.major, right.major)
	}
	if left.minor != right.minor {
		return intCmp(left.minor, right.minor)
	}
	if left.patch != right.patch {
		return intCmp(left.patch, right.patch)
	}
	if len(left.pre) == 0 && len(right.pre) == 0 {
		return 0
	}
	if len(left.pre) == 0 {
		return 1
	}
	if len(right.pre) == 0 {
		return -1
	}
	for i := 0; i < len(left.pre) && i < len(right.pre); i++ {
		if left.pre[i] == right.pre[i] {
			continue
		}
		ln, lerr := strconv.Atoi(left.pre[i])
		rn, rerr := strconv.Atoi(right.pre[i])
		if lerr == nil && rerr == nil {
			return intCmp(ln, rn)
		}
		if lerr == nil {
			return -1
		}
		if rerr == nil {
			return 1
		}
		if left.pre[i] < right.pre[i] {
			return -1
		}
		return 1
	}
	return intCmp(len(left.pre), len(right.pre))
}

func intCmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// StageUpdate downloads the archive and checksum file, verifies the SHA-256
// digest (and the cosign bundle when cosign is installed or required), then
// extracts nexusd into the staged directory.
func StageUpdate(ctx context.Context, info UpdateInfo, stagedDir string, requireSignature bool) (string, error) {
	if info.DownloadURL == "" || info.ChecksumURL == "" {
		return "", errors.New("release metadata is incomplete")
	}
	tmp, err := os.MkdirTemp("", "agentnexus-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, info.AssetName)
	checksums := filepath.Join(tmp, "checksums.txt")
	if err := downloadFile(ctx, info.DownloadURL, archive); err != nil {
		return "", err
	}
	if err := downloadFile(ctx, info.ChecksumURL, checksums); err != nil {
		return "", err
	}
	if err := verifyChecksum(archive, checksums); err != nil {
		return "", err
	}
	if err := verifyCosign(ctx, checksums, info.SignatureURL, requireSignature); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stagedDir, 0o700); err != nil {
		return "", err
	}
	binaryName := "nexusd"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	target := filepath.Join(stagedDir, binaryName)
	if strings.HasSuffix(info.AssetName, ".zip") {
		err = extractZipBinary(archive, binaryName, target)
	} else {
		err = extractTarBinary(archive, binaryName, target)
	}
	if err != nil {
		return "", err
	}
	_ = os.Chmod(target, 0o755)
	return target, nil
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AgentNexus-Updater")
	if token := githubToken(); token != "" && strings.Contains(url, "github.com") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, io.LimitReader(resp.Body, 256<<20))
	return err
}

func verifyChecksum(archive, checksums string) error {
	data, err := os.ReadFile(checksums)
	if err != nil {
		return err
	}
	name := filepath.Base(archive)
	var want string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt does not contain %s", name)
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func verifyCosign(ctx context.Context, checksums, signatureURL string, required bool) error {
	path, err := exec.LookPath("cosign")
	if err != nil {
		if required {
			return errors.New("cosign is required to verify this update; install cosign or pass --allow-checksum-only")
		}
		return nil
	}
	if signatureURL == "" {
		if required {
			return errors.New("release has no cosign bundle")
		}
		return nil
	}
	bundle := checksums + ".sigstore.json"
	if err := downloadFile(ctx, signatureURL, bundle); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, "verify-blob",
		"--bundle", bundle,
		"--certificate-identity-regexp", `^https://github\.com/AlexanderWagnerDev/agent-nexus/\.github/workflows/release\.yml@refs/tags/`,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		checksums)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cosign verification failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func extractZipBinary(path, name, target string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != name {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		return writeNewBinary(target, src)
	}
	return fmt.Errorf("%s not found in archive", name)
}

func extractTarBinary(path, name, target string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(header.Name) != name || header.Typeflag != tar.TypeReg {
			continue
		}
		return writeNewBinary(target, reader)
	}
	return fmt.Errorf("%s not found in archive", name)
}

func writeNewBinary(target string, src io.Reader) error {
	tmp := target + ".new"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(src, 128<<20)); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// ServiceInstalled reports whether the per-user service that can restart the
// daemon after a binary replacement is present.
func ServiceInstalled() bool {
	return serviceInstalled()
}
