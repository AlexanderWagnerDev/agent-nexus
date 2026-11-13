package host

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestUpdateChannelSelection(t *testing.T) {
	assetFor := func(version string) []map[string]string {
		return []map[string]string{
			{"name": releaseAssetName(version, runtime.GOOS, runtime.GOARCH), "browser_download_url": "https://example.invalid/asset"},
			{"name": "checksums.txt", "browser_download_url": "https://example.invalid/checksums"},
		}
	}
	releases := []map[string]any{
		{"tag_name": "v1.0.0", "prerelease": false, "assets": assetFor("1.0.0")},
		{"tag_name": "v2.0.0-rc.1", "prerelease": true, "assets": assetFor("2.0.0-rc.1")},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("NEXUS_UPDATE_API", server.URL)
	t.Setenv("NEXUS_UPDATE_REPO", "example/repo")

	info, err := CheckForUpdate(context.Background(), "0.9.0", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if info.Latest != "1.0.0" || !info.Available || info.Prerelease {
		t.Fatalf("stable channel selected %+v", info)
	}
	info, err = CheckForUpdate(context.Background(), "0.9.0", "prerelease")
	if err != nil {
		t.Fatal(err)
	}
	if info.Latest != "2.0.0-rc.1" || !info.Prerelease {
		t.Fatalf("prerelease channel selected %+v", info)
	}

	// A repository with only prereleases must not silently feed a stable host.
	releases = []map[string]any{{"tag_name": "v2.0.0-rc.1", "prerelease": true, "assets": assetFor("2.0.0-rc.1")}}
	if _, err := CheckForUpdate(context.Background(), "1.0.0", "stable"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("stable accepted a prerelease-only feed: %v", err)
	}
}

func TestCheckForUpdateUsesETag(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		asset := releaseAssetName("1.2.3", runtime.GOOS, runtime.GOARCH)
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name": "v1.2.3", "prerelease": false,
			"assets": []map[string]string{
				{"name": asset, "browser_download_url": "https://example.invalid/asset"},
				{"name": "checksums.txt", "browser_download_url": "https://example.invalid/checksums"},
			},
		}})
	}))
	defer server.Close()
	t.Setenv("NEXUS_UPDATE_API", server.URL)
	t.Setenv("NEXUS_UPDATE_REPO", "example/repo")

	info, etag, modified, err := CheckForUpdateCached(context.Background(), "1.0.0", "stable", "")
	if err != nil || modified || etag != `"v1"` || info.Latest != "1.2.3" {
		t.Fatalf("first check: info=%+v etag=%q modified=%v err=%v", info, etag, modified, err)
	}
	_, _, modified, err = CheckForUpdateCached(context.Background(), "1.0.0", "stable", etag)
	if err != nil || !modified {
		t.Fatalf("conditional check: modified=%v err=%v", modified, err)
	}
	if requests != 2 {
		t.Fatalf("expected two requests, got %d", requests)
	}
}

func TestReleaseAssetNamesMatchGoReleaser(t *testing.T) {
	cases := []struct {
		goos, goarch, ext string
	}{
		{"windows", "amd64", ".zip"},
		{"windows", "arm64", ".zip"},
		{"linux", "amd64", ".tar.gz"},
		{"linux", "arm64", ".tar.gz"},
		{"darwin", "amd64", ".tar.gz"},
		{"darwin", "arm64", ".tar.gz"},
	}
	for _, tc := range cases {
		got := releaseAssetName("0.1.0", tc.goos, tc.goarch)
		want := "agent-nexus_0.1.0_" + tc.goos + "_" + tc.goarch + tc.ext
		if got != want {
			t.Errorf("releaseAssetName(%s/%s)=%q want %q", tc.goos, tc.goarch, got, want)
		}
	}
	// A prerelease tag must keep its suffix in the archive name.
	if got := releaseAssetName("0.1.0-rc.1", "linux", "amd64"); got != "agent-nexus_0.1.0-rc.1_linux_amd64.tar.gz" {
		t.Errorf("prerelease asset name = %q", got)
	}
}

func TestUpdateSettingsDefaults(t *testing.T) {
	prefs := (Config{}).UpdatePrefs()
	if prefs.Disabled || prefs.Channel != UpdateChannelStable {
		t.Fatalf("unexpected defaults: %+v", prefs)
	}
	if !prefs.InstallIdle() || !prefs.SignatureRequired() {
		t.Fatalf("idle installation or signatures disabled by default: %+v", prefs)
	}
	if prefs.Interval() != DefaultUpdateInterval {
		t.Fatalf("interval = %v", prefs.Interval())
	}
	short := UpdateSettings{CheckIntervalMinutes: 1}.Resolve()
	if short.Interval() != MinUpdateInterval {
		t.Fatalf("short interval was not clamped: %v", short.Interval())
	}
	disabled := false
	explicit := UpdateSettings{InstallWhenIdle: &disabled, RequireSignature: &disabled}.Resolve()
	if explicit.InstallIdle() || explicit.SignatureRequired() {
		t.Fatalf("explicit opt-outs were ignored: %+v", explicit)
	}
	explicit.BlockedVersions = []string{"1.2.3"}
	if !explicit.IsBlocked("v1.2.3") {
		t.Fatal("blocked version not recognized")
	}
}

func TestNextUpdateDelayBackoff(t *testing.T) {
	base := 6 * time.Hour
	for failures, wantMin := range map[int]time.Duration{
		0:  6 * time.Hour,
		1:  12 * time.Hour,
		2:  24 * time.Hour,
		10: MaxUpdateInterval,
	} {
		got := nextUpdateDelay(base, failures)
		if got < wantMin || got > wantMin+wantMin/5 {
			t.Errorf("failures=%d delay=%v want between %v and %v", failures, got, wantMin, wantMin+wantMin/5)
		}
	}
}

func TestUpdateStatePersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	state := updateState{
		Phase: UpdatePhaseSuccess, Channel: UpdatePhaseSuccess,
		LastSuccess: time.Now().UTC().Truncate(time.Second),
		Info:        UpdateInfo{Current: "1.0.0", Latest: "1.2.0"},
	}
	if err := saveUpdateState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadUpdateState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != state.Phase || loaded.Info.Latest != state.Info.Latest || !loaded.LastSuccess.Equal(state.LastSuccess) {
		t.Fatalf("state round-trip mismatch: %+v", loaded)
	}
	path, err := updateStatePath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("update state is readable by other users: %o", info.Mode().Perm())
		}
	}
}

func TestTarExtractionFindsBinaryAndIgnoresSymlinks(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "nexusd", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777})
	body := []byte("real-binary")
	_ = tw.WriteHeader(&tar.Header{Name: "nexusd", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "staged")
	if err := extractTarBinary(archivePath, "nexusd", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(body) {
		t.Fatalf("extracted %q, err=%v", got, err)
	}
}

func TestUpdateGateRejectsNewRuns(t *testing.T) {
	d := silentDaemon(echoTestAdapter{})
	d.updating.Store(true)
	if d.reserveRunLocked("echo:new:test", "run-1") {
		t.Fatal("a new run was reserved while an update was installing")
	}
	d.updating.Store(false)
	if !d.reserveRunLocked("echo:new:test", "run-1") {
		t.Fatal("run reservation stayed blocked after the update")
	}
}

func TestResumeUpdateVerificationBlocksRepeatedMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	d := silentDaemon(echoTestAdapter{})
	state := updateState{
		Phase: UpdatePhaseVerifying, TargetVersion: "9.9.9",
		PhaseAt: time.Now().UTC(), VerifyAt: time.Now().UTC().Add(time.Minute),
	}
	if err := saveUpdateState(state); err != nil {
		t.Fatal(err)
	}
	d.resumeUpdateVerification(context.Background())
	loaded, err := loadUpdateState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != UpdatePhaseBlocked || loaded.BlockedVersion != "9.9.9" {
		t.Fatalf("repeated failed starts were not blocked: %+v", loaded)
	}
}

func TestWaitForIdleAndGate(t *testing.T) {
	d := silentDaemon(echoTestAdapter{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !d.waitForIdleAndGate(ctx, 3*time.Second) {
		t.Fatal("idle host did not gate for an update")
	}
	if !d.updating.Load() {
		t.Fatal("update gate was not set")
	}
	d.updating.Store(false)
}

func TestVerifyStagedBinaryRejectsMissingFile(t *testing.T) {
	if err := VerifyStagedBinary(context.Background(), filepath.Join(t.TempDir(), "missing"), "1.0.0"); err == nil {
		t.Fatal("missing staged binary passed the version check")
	}
}
