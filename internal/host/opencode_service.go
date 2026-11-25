package host

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// openCodeService is the local HTTP server an OpenCode desktop or TUI client
// already runs. Reusing it keeps the native chat live: prompts and responses
// flow through the same event stream instead of a second server that writes
// the same database without notifying the first one.
type openCodeService struct {
	URL      string `json:"url"`
	Password string `json:"password"`
	PID      int    `json:"pid"`
}

func openCodeStateDir(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name, args, err := commandFor(bin, []string{"debug", "paths"})
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "state" {
			return strings.Join(fields[1:], " ")
		}
	}
	return ""
}

// openCodeServiceInfo reads the service file and verifies that the service is
// reachable on loopback. A stale file or a crashed client falls back to a
// private server, which is the previous behaviour.
func openCodeServiceInfo(bin string) (openCodeService, bool) {
	dir := openCodeStateDir(bin)
	if dir == "" {
		return openCodeService{}, false
	}
	return readOpenCodeService(dir)
}

func readOpenCodeService(dir string) (openCodeService, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "service.json"))
	if err != nil {
		return openCodeService{}, false
	}
	var svc openCodeService
	if json.Unmarshal(raw, &svc) != nil || svc.URL == "" || svc.Password == "" {
		return openCodeService{}, false
	}
	parsed, err := url.Parse(svc.URL)
	if err != nil || parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
		return openCodeService{}, false
	}
	svc.URL = strings.TrimRight(svc.URL, "/")
	if !probeOpenCodeService(svc) {
		return openCodeService{}, false
	}
	return svc, true
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func probeOpenCodeService(svc openCodeService) bool {
	req, err := http.NewRequest(http.MethodGet, svc.URL+"/api/session", nil)
	if err != nil {
		return false
	}
	req.SetBasicAuth("opencode", svc.Password)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
