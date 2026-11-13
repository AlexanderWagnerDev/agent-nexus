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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	Channel      string    `json:"channel,omitempty"`
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

// NormalizeUpdateChannel resolves auto and unknown channel names. Stable is
// always the safe default; an unknown value must never silently select
// prereleases.
func NormalizeUpdateChannel(channel, currentVersion string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case UpdateChannelStable:
		return UpdateChannelStable
	case UpdateChannelPrerelease:
		return UpdateChannelPrerelease
	case UpdateChannelDevelopment:
		return UpdateChannelDevelopment
	case "auto", "":
		if strings.Contains(strings.TrimPrefix(currentVersion, "v"), "-") {
			return UpdateChannelPrerelease
		}
		return UpdateChannelStable
	default:
		return UpdateChannelStable
	}
}

// channelAllows reports whether a release may be installed on a channel.
// "stable" never accepts prereleases; "development" uses the same published
// release feed as prerelease because development builds are workflow
// artifacts, not Git tags.
func channelAllows(channel string, prerelease bool) bool {
	return NormalizeUpdateChannel(channel, "") == UpdateChannelStable && !prerelease ||
		NormalizeUpdateChannel(channel, "") != UpdateChannelStable
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
	info, _, _, err := CheckForUpdateCached(ctx, currentVersion, channel, "")
	return info, err
}

// CheckForUpdateCached is CheckForUpdate with an ETag for conditional
// requests. notModified is true when GitHub answered 304; the caller should
// reuse its cached metadata.
func CheckForUpdateCached(ctx context.Context, currentVersion, channel, etag string) (info UpdateInfo, newETag string, notModified bool, err error) {
	info = UpdateInfo{Current: currentVersion, Channel: NormalizeUpdateChannel(channel, currentVersion)}
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", updateAPIBase(), updateRepo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return info, "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AgentNexus-Updater")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := updateHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return info, "", false, err
	}
	defer resp.Body.Close()
	newETag = resp.Header.Get("ETag")
	if resp.StatusCode == http.StatusNotModified {
		return info, newETag, true, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return info, newETag, false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return info, newETag, false, errors.New("release repository not found; a private repository needs NEXUS_GITHUB_TOKEN with release read access")
	}
	if resp.StatusCode != http.StatusOK {
		return info, newETag, false, fmt.Errorf("release check failed: HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return info, newETag, false, err
	}
	channel = NormalizeUpdateChannel(channel, currentVersion)
	var newest *githubRelease
	for i := range releases {
		release := &releases[i]
		if release.Draft || release.TagName == "" || !channelAllows(channel, release.Prerelease) {
			continue
		}
		if !validReleaseVersion(release.TagName) {
			continue
		}
		if newest == nil || compareVersions(strings.TrimPrefix(release.TagName, "v"), strings.TrimPrefix(newest.TagName, "v")) > 0 {
			newest = release
		}
	}
	if newest == nil {
		return info, newETag, false, ErrNoRelease
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
	if info.Available && info.DownloadURL == "" {
		return info, newETag, false, fmt.Errorf("release %s has no asset %s", version, name)
	}
	return info, newETag, false, nil
}

// validReleaseVersion accepts semver-like tags and rejects arbitrary labels
// that would otherwise compare as 0.0.0.
func validReleaseVersion(tag string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if v == "" || v[0] < '0' || v[0] > '9' {
		return false
	}
	return true
}

// updateHTTPClient returns the HTTP client used for release metadata and
// downloads. Redirects may not downgrade to plain HTTP; Go already drops
// Authorization headers when a redirect crosses to another host.
func updateHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return fmt.Errorf("unsupported redirect scheme %q", req.URL.Scheme)
			}
			if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return errors.New("refusing HTTPS-to-HTTP redirect for an update download")
			}
			if !githubTokenHost(req.URL.String()) {
				// Never forward a GitHub token to a redirect target that is not
				// an allowed GitHub host, even if a future Go version changes
				// the default header policy.
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
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
	target, _, err := StageUpdateWithMode(ctx, info, stagedDir, requireSignature)
	return target, err
}

// StageUpdateWithMode also reports the trust mode that was actually used:
// "cosign" or "checksum-only". A failed signature is never downgraded to
// checksum-only.
func StageUpdateWithMode(ctx context.Context, info UpdateInfo, stagedDir string, requireSignature bool) (string, string, error) {
	return StageUpdateNamed(ctx, info, stagedDir, "nexusd", requireSignature)
}

// StageUpdateNamed stages one executable from a release archive. It is shared
// by the daemon and the terminal client so both use the same checksum,
// signature and extraction rules.
func StageUpdateNamed(ctx context.Context, info UpdateInfo, stagedDir, binaryName string, requireSignature bool) (string, string, error) {
	if info.DownloadURL == "" || info.ChecksumURL == "" {
		return "", "", errors.New("release metadata is incomplete")
	}
	if binaryName == "" || filepath.Base(binaryName) != binaryName {
		return "", "", errors.New("invalid staged binary name")
	}
	tmp, err := os.MkdirTemp("", "agentnexus-update-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, info.AssetName)
	checksums := filepath.Join(tmp, "checksums.txt")
	if err := downloadFile(ctx, info.DownloadURL, archive); err != nil {
		return "", "", err
	}
	if err := downloadFile(ctx, info.ChecksumURL, checksums); err != nil {
		return "", "", err
	}
	if err := verifyChecksum(archive, checksums); err != nil {
		return "", "", err
	}
	mode, err := verifyCosign(ctx, checksums, info.SignatureURL, requireSignature)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(stagedDir, 0o700); err != nil {
		return "", "", err
	}
	binaryName = stagedBinaryName(binaryName)
	target := filepath.Join(stagedDir, binaryName)
	if strings.HasSuffix(info.AssetName, ".zip") {
		err = extractZipBinary(archive, binaryName, target)
	} else {
		err = extractTarBinary(archive, binaryName, target)
	}
	if err != nil {
		return "", "", err
	}
	_ = os.Chmod(target, 0o755)
	return target, mode, nil
}

func stagedBinaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// VerifyStagedBinary runs the downloaded daemon's version command before it
// can replace a working installation. A wrong or non-executable binary is
// rejected here, not after the service restart.
func VerifyStagedBinary(ctx context.Context, path, expected string) error {
	return VerifyStagedBinaryNamed(ctx, path, "nexusd", expected)
}

// VerifyStagedBinaryNamed checks the version output of a staged executable.
func VerifyStagedBinaryNamed(ctx context.Context, path, name, expected string) error {
	if expected == "" {
		return errors.New("expected version is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("staged binary failed its version check: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != name {
		return fmt.Errorf("staged binary reported an unexpected version: %s", strings.TrimSpace(string(out)))
	}
	got := strings.TrimPrefix(fields[1], "v")
	want := strings.TrimPrefix(strings.TrimSpace(expected), "v")
	if got != want {
		return fmt.Errorf("staged binary version %s does not match release %s", got, want)
	}
	return nil
}

// githubTokenHost reports whether a download URL may receive the GitHub token.
// Matching the parsed hostname prevents a crafted asset URL such as
// https://evil.example/github.com from receiving the credential. A custom
// release API (NEXUS_UPDATE_API) may receive the token on its own host so
// private mirrors keep working.
func githubTokenHost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return parsed.Scheme == "https"
	}
	if base, err := url.Parse(updateAPIBase()); err == nil && base.Hostname() != "" {
		if strings.EqualFold(host, base.Hostname()) && parsed.Scheme == base.Scheme {
			return true
		}
	}
	return false
}

func downloadFile(ctx context.Context, rawURL, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AgentNexus-Updater")
	if token := githubToken(); token != "" && githubTokenHost(rawURL) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := updateHTTPClient(5 * time.Minute).Do(req)
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
	const maxDownload = int64(256 << 20)
	written, err := io.Copy(file, io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	if written > maxDownload {
		file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("download exceeds the %d MiB limit", maxDownload>>20)
	}
	return file.Close()
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

func verifyCosign(ctx context.Context, checksums, signatureURL string, required bool) (string, error) {
	path, err := exec.LookPath("cosign")
	if err != nil {
		if required {
			return "", errors.New("cosign is required to verify this update; install cosign or set updates.require_signature=false (checksum-only)")
		}
		return "checksum-only", nil
	}
	if signatureURL == "" {
		if required {
			return "", errors.New("release has no cosign bundle; refusing a signature-required update")
		}
		return "checksum-only", nil
	}
	bundle := checksums + ".sigstore.json"
	if err := downloadFile(ctx, signatureURL, bundle); err != nil {
		return "", err
	}
	identity := `^https://github\.com/` + regexp.QuoteMeta(updateRepo()) + `/\.github/workflows/release\.yml@refs/tags/`
	cmd := exec.CommandContext(ctx, path, "verify-blob",
		"--bundle", bundle,
		"--certificate-identity-regexp", identity,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		checksums)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cosign verification failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "cosign", nil
}

func extractZipBinary(path, name, target string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != name || file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if file.UncompressedSize64 > 128<<20 {
			return fmt.Errorf("archive entry %s exceeds the extraction limit", file.Name)
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		err = writeNewBinary(target, src)
		src.Close()
		return err
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
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != name {
			continue
		}
		if header.Size > 128<<20 {
			return fmt.Errorf("archive entry %s exceeds the extraction limit", header.Name)
		}
		return writeNewBinary(target, reader)
	}
	return fmt.Errorf("%s not found in archive", name)
}

func writeNewBinary(target string, src io.Reader) error {
	file, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".new-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	written, err := io.Copy(file, io.LimitReader(src, 128<<20+1))
	if err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if written > 128<<20 {
		file.Close()
		os.Remove(tmp)
		return errors.New("extracted binary exceeds the 128 MiB limit")
	}
	if err := file.Chmod(0o755); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

// ServiceInstalled reports whether the per-user service that can restart the
// daemon after a binary replacement is present.
func ServiceInstalled() bool {
	return serviceInstalled()
}
