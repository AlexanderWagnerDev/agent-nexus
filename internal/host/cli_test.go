package host

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPairAgainstFakeHub(t *testing.T) {
	t.Setenv("NEXUSD_CONFIG", filepath.Join(t.TempDir(), "nexusd.json"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/hosts/pair":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["code"] != "ABCD-EFGH" || req["public_key"] == "" || req["os"] == "" {
				t.Errorf("pair request: %+v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"host_id":"host-1","sas":"123-456"}`))
		case "/api/v1/hub-key":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg, sas, err := Pair(context.Background(), server.URL, "abcd-efgh", "demo-host")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HostID != "host-1" || cfg.HostName != "demo-host" || sas != "123-456" {
		t.Fatalf("pair result: %+v %q", cfg, sas)
	}
	if _, err := cfg.SigningKey(); err != nil {
		t.Fatalf("saved key is invalid: %v", err)
	}
	loaded, err := LoadConfig()
	if err != nil || loaded.HostID != "host-1" {
		t.Fatalf("load config: %+v %v", loaded, err)
	}
}

func TestPairRejectsHubError(t *testing.T) {
	t.Setenv("NEXUSD_CONFIG", filepath.Join(t.TempDir(), "nexusd.json"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"pairing code is invalid"}`))
	}))
	defer server.Close()
	if _, _, err := Pair(context.Background(), server.URL, "bad", "demo"); err == nil || !strings.Contains(err.Error(), "pairing code is invalid") {
		t.Fatalf("pair error: %v", err)
	}
}

func TestDoctorReportsConfigurationProblems(t *testing.T) {
	t.Setenv("NEXUSD_CONFIG", filepath.Join(t.TempDir(), "nexusd.json"))
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	issues := Doctor(Config{}, context.Canceled, &out)
	if issues == 0 || !strings.Contains(out.String(), "NOT PAIRED") {
		t.Fatalf("not-paired doctor: issues=%d output=%s", issues, out.String())
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	cfg := Config{
		HubURL: server.URL, HostID: "host-1", HostName: "demo",
		PrivateKey:   base64.StdEncoding.EncodeToString(private),
		HubPublicKey: base64.StdEncoding.EncodeToString(public),
		Policy:       Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"codex"}},
	}
	out.Reset()
	_ = Doctor(cfg, nil, &out)
	if !strings.Contains(out.String(), "identity:   ok") || !strings.Contains(out.String(), "hub health: HTTP 200") {
		t.Fatalf("doctor output: %s", out.String())
	}
	if !strings.Contains(out.String(), "search index:") {
		t.Fatalf("doctor did not report the search index: %s", out.String())
	}

	cfg.PrivateKey = "short"
	cfg.Policy.AllowedRoots = []string{filepath.Join(root, "missing")}
	out.Reset()
	issues = Doctor(cfg, nil, &out)
	if issues == 0 || !strings.Contains(out.String(), "BROKEN") || !strings.Contains(out.String(), "does not exist") {
		t.Fatalf("broken doctor: issues=%d output=%s", issues, out.String())
	}
}
