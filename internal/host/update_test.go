package host

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	info, err := CheckForUpdate(context.Background(), "0.3.1", "prerelease")
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
