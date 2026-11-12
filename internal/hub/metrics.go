package hub

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metrics is a deliberately small Prometheus text-format exporter. It avoids
// a client-library dependency and keeps labels to closed sets (route patterns,
// status codes) so cardinality stays bounded.
type metrics struct {
	mu              sync.Mutex
	requests        map[string]int64
	durationSum     map[string]float64
	durationCount   map[string]int64
	hostReconnects  uint64
	pushFailures    uint64
	approvalSeconds float64
	approvals       int64
	approvalStart   map[string]time.Time
}

func newMetrics() *metrics {
	return &metrics{
		requests:      map[string]int64{},
		durationSum:   map[string]float64{},
		durationCount: map[string]int64{},
		approvalStart: map[string]time.Time{},
	}
}

func (m *metrics) observeRequest(route string, status int, d time.Duration) {
	key := route + "\x00" + strconv.Itoa(status)
	m.mu.Lock()
	m.requests[key]++
	m.durationSum[route] += d.Seconds()
	m.durationCount[route]++
	m.mu.Unlock()
}

func (m *metrics) hostReconnected() {
	m.mu.Lock()
	m.hostReconnects++
	m.mu.Unlock()
}

func (m *metrics) pushFailed() {
	m.mu.Lock()
	m.pushFailures++
	m.mu.Unlock()
}

func (m *metrics) approvalRequested(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	m.approvalStart[id] = time.Now()
	m.mu.Unlock()
}

func (m *metrics) approvalAnswered(id string) {
	m.mu.Lock()
	start, ok := m.approvalStart[id]
	if ok {
		delete(m.approvalStart, id)
		m.approvalSeconds += time.Since(start).Seconds()
		m.approvals++
	}
	m.mu.Unlock()
}

type httpMetricsWriter struct {
	http.ResponseWriter
	status int
}

func (w *httpMetricsWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *httpMetricsWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *httpMetricsWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer, which is
// how coder/websocket upgrades an HTTP/1 request.
func (w *httpMetricsWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *httpMetricsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// observeHTTP wraps the mux. It records the matched route pattern, never the
// raw URL, so session ids and search queries do not leak into metrics labels.
func (s *Server) observeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 64 {
			requestID = randomToken(12)
		}
		w.Header().Set("X-Request-ID", requestID)
		rec := &httpMetricsWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		route := r.Pattern
		if route == "" {
			route = "asset"
		}
		s.metrics.observeRequest(route, status, time.Since(start))
		levelAttrs := []any{"request_id", requestID, "method", r.Method, "route", route, "status", status, "duration_ms", time.Since(start).Milliseconds()}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.log.Info("http request", levelAttrs...)
		} else {
			s.log.Debug("http request", levelAttrs...)
		}
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MetricsToken == "" {
		writeError(w, http.StatusForbidden, "metrics token is not configured")
		return
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	want := "Bearer " + s.cfg.MetricsToken
	if subtle.ConstantTimeCompare([]byte(auth), []byte(want)) != 1 {
		writeError(w, http.StatusUnauthorized, "metrics token required")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	s.mu.RLock()
	hosts := len(s.hosts)
	clients := len(s.clients)
	s.mu.RUnlock()
	runs, _ := s.store.CountActiveRuns()
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP nexus_hosts_connected Hosts with a live outbound link.\n# TYPE nexus_hosts_connected gauge\nnexus_hosts_connected %d\n", hosts)
	fmt.Fprintf(&b, "# HELP nexus_active_runs Runs without an end timestamp.\n# TYPE nexus_active_runs gauge\nnexus_active_runs %d\n", runs)
	fmt.Fprintf(&b, "# HELP nexus_browser_websockets Connected browser WebSockets.\n# TYPE nexus_browser_websockets gauge\nnexus_browser_websockets %d\n", clients)
	s.metrics.mu.Lock()
	fmt.Fprintf(&b, "# HELP nexus_host_reconnects_total Host links that replaced an older link.\n# TYPE nexus_host_reconnects_total counter\nnexus_host_reconnects_total %d\n", s.metrics.hostReconnects)
	fmt.Fprintf(&b, "# HELP nexus_push_failures_total Web Push delivery failures.\n# TYPE nexus_push_failures_total counter\nnexus_push_failures_total %d\n", s.metrics.pushFailures)
	fmt.Fprintf(&b, "# HELP nexus_approval_latency_seconds Time between approval request and decision.\n# TYPE nexus_approval_latency_seconds summary\n")
	fmt.Fprintf(&b, "nexus_approval_latency_seconds_sum %g\nnexus_approval_latency_seconds_count %d\n", s.metrics.approvalSeconds, s.metrics.approvals)
	requestKeys := make([]string, 0, len(s.metrics.requests))
	for key := range s.metrics.requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Strings(requestKeys)
	fmt.Fprintf(&b, "# HELP nexus_http_requests_total HTTP requests by route and status.\n# TYPE nexus_http_requests_total counter\n")
	for _, key := range requestKeys {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		fmt.Fprintf(&b, "nexus_http_requests_total{route=%q,status=%q} %d\n", parts[0], parts[1], s.metrics.requests[key])
	}
	routes := make([]string, 0, len(s.metrics.durationSum))
	for route := range s.metrics.durationSum {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	fmt.Fprintf(&b, "# HELP nexus_http_request_duration_seconds HTTP request duration by route.\n# TYPE nexus_http_request_duration_seconds summary\n")
	for _, route := range routes {
		fmt.Fprintf(&b, "nexus_http_request_duration_seconds_sum{route=%q} %g\n", route, s.metrics.durationSum[route])
		fmt.Fprintf(&b, "nexus_http_request_duration_seconds_count{route=%q} %d\n", route, s.metrics.durationCount[route])
	}
	s.metrics.mu.Unlock()
	_, _ = w.Write([]byte(b.String()))
}
