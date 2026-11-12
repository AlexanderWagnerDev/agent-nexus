package hub

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMetricsRequireTokenAndExposeBoundedLabels(t *testing.T) {
	hub := newAPITestHub(t, Config{MetricsEnabled: true, MetricsToken: "scrape-secret"})
	status, body := hub.do(nil, http.MethodGet, "/metrics", nil, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "metrics without token")

	req, err := http.NewRequest(http.MethodGet, hub.http.URL+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer scrape-secret")
	res, err := hub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("metrics status: %d %s", res.StatusCode, raw)
	}
	text := string(raw)
	for _, want := range []string{
		"nexus_hosts_connected",
		"nexus_active_runs",
		"nexus_browser_websockets",
		"nexus_http_requests_total",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %s:\n%s", want, text)
		}
	}
	// Route patterns must not include path parameters or query strings.
	if strings.Contains(text, "{id}") {
		t.Fatalf("metrics leaked a route pattern: %s", text)
	}
}

func TestMetricsDisabledByDefault(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	status, body := hub.do(nil, http.MethodGet, "/metrics", nil, nil)
	expectStatus(t, status, http.StatusNotFound, body, "metrics disabled")
}
