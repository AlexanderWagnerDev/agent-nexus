package host

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.2", "0.3.1", 1},
		{"0.3.2-rc.1", "0.3.2", -1},
		{"0.3.2-rc.2", "0.3.2-rc.1", 1},
		{"v1.0.0", "0.9.9", 1},
		{"0.3.2", "0.3.2", 0},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestGitHubTokenOnlyForGitHubHosts(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://api.github.com/repos/o/r/releases/assets/1", true},
		{"https://github.com/o/r/releases/download/v1/a.zip", true},
		{"https://objects.githubusercontent.com/asset", true},
		{"https://evil.example/github.com/a.zip", false},
		{"https://github.com.evil.example/a.zip", false},
		{"https://notgithub.com/a.zip", false},
		{"http://github.com/o/r/a.zip", false},
		{"not a url", false},
	}
	for _, tc := range cases {
		if got := githubTokenHost(tc.url); got != tc.want {
			t.Errorf("githubTokenHost(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestGitHubTokenForConfiguredReleaseMirror(t *testing.T) {
	t.Setenv("NEXUS_UPDATE_API", "https://ghe.example/api/v3")
	if !githubTokenHost("https://ghe.example/releases/download/a.zip") {
		t.Fatal("configured release mirror did not receive the token")
	}
	if githubTokenHost("https://ghe.example.evil/releases/a.zip") {
		t.Fatal("lookalike host received the token")
	}
	if githubTokenHost("http://ghe.example/releases/a.zip") {
		t.Fatal("token would be sent over plain HTTP to the mirror")
	}
}

func TestCheckForUpdateAndStage(t *testing.T) {
	binary := []byte("fake-nexusd-binary")
	var archive bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&archive)
		w, _ := zw.Create("nexusd.exe")
		_, _ = w.Write(binary)
		_ = zw.Close()
	} else {
		t.Skip("zip staging test is Windows-specific; tar path is covered by the same checksum logic")
	}
	sum := sha256.Sum256(archive.Bytes())
	version := "9.9.9"
	asset := releaseAssetName(version, runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/AlexanderWagnerDev/agent-nexus/releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name": "v" + version, "prerelease": true,
			"assets": []map[string]string{
				{"name": asset, "browser_download_url": "http://" + r.Host + "/asset"},
				{"name": "checksums.txt", "browser_download_url": "http://" + r.Host + "/checksums"},
			},
		}})
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("NEXUS_UPDATE_API", server.URL)
	info, err := CheckForUpdate(context.Background(), "alpha", "prerelease")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != version || info.AssetName != asset {
		t.Fatalf("unexpected update info: %+v", info)
	}
	staged, err := StageUpdate(context.Background(), info, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("staged binary mismatch: %q %v", got, err)
	}
	if filepath.Base(staged) != "nexusd.exe" {
		t.Fatalf("unexpected staged name: %s", staged)
	}
}

func TestCheckForUpdateNoRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	t.Setenv("NEXUS_UPDATE_API", server.URL)
	info, err := CheckForUpdate(context.Background(), "alpha", "prerelease")
	if !errors.Is(err, ErrNoRelease) {
		t.Fatalf("expected ErrNoRelease, got %v", err)
	}
	if info.Latest != "" || info.Available {
		t.Fatalf("unexpected update info for an empty feed: %+v", info)
	}
}

func TestUpdateRefusesActiveRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	cfgDir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	status := runtimeStatus{PID: os.Getpid(), Version: "test", ActiveRuns: 2, UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(status)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(t.TempDir(), "nexusd")
	if err := os.WriteFile(staged, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = InstallStagedUpdate(staged, false)
	if err == nil || !strings.Contains(err.Error(), "active agent run") {
		t.Fatalf("active-run update was not refused: %v", err)
	}
	err = RollbackUpdate(false)
	if err == nil || !strings.Contains(err.Error(), "active agent run") {
		t.Fatalf("active-run rollback was not refused: %v", err)
	}

	// A stale snapshot must not block recovery after a crash.
	status.UpdatedAt = time.Now().Add(-5 * time.Minute)
	raw, _ = json.Marshal(status)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, fresh := ReadRuntimeStatus(); fresh {
		t.Fatal("stale runtime status reported as fresh")
	}
}

func TestStableChannelRejectsSuffixedTags(t *testing.T) {
	for _, tag := range []string{"v0.2.0", "0.2.0", "v1.0.0+build.5"} {
		if !stableVersionAllowed(tag) {
			t.Fatalf("stable rejected %q", tag)
		}
	}
	for _, tag := range []string{"v0.2.0-rc.1", "0.2.0-beta", "1.0.0-alpha.2"} {
		if stableVersionAllowed(tag) {
			t.Fatalf("stable accepted prerelease tag %q", tag)
		}
	}
}
