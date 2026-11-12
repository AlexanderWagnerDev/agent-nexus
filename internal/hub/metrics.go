package hub

import (
	"bufio"
	"context"
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

// approvalMetricCap bounds the pending-approval bookkeeping. Beyond this
// point the oldest entry is evicted; a missing metric is preferable to an
// unbounded map after a bug or a huge number of abandoned approvals.
const approvalMetricCap = 10000

type approvalMetric struct {
	start time.Time
	runID string
	seq   uint64
}

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
	protocolErrors  uint64
	runLimitRejects uint64
	approvalSeconds float64
	approvals       int64
	// approvalStart is keyed by run ID and approval ID: hosts may reuse an
	// approval ID in a different run.
	approvalStart map[string]approvalMetric
	approvalByRun map[string]map[string]struct{}
	approvalSeq   uint64
}

func newMetrics() *metrics {
	return &metrics{
		requests:      map[string]int64{},
		durationSum:   map[string]float64{},
		durationCount: map[string]int64{},
		approvalStart: map[string]approvalMetric{},
		approvalByRun: map[string]map[string]struct{}{},
	}
}

func approvalKey(runID, id string) string {
	return runID + "\x00" + id
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

func (m *metrics) protocolError() {
	m.mu.Lock()
	m.protocolErrors++
	m.mu.Unlock()
}

func (m *metrics) runLimitRejected() {
	m.mu.Lock()
	m.runLimitRejects++
	m.mu.Unlock()
}

// approvalRequested records the start of one approval. If the map is full the
// oldest entry is dropped before inserting the new one.
func (m *metrics) approvalRequested(runID, id string) {
	if id == "" {
		return
	}
	key := approvalKey(runID, id)
	m.mu.Lock()
	if _, exists := m.approvalStart[key]; !exists && len(m.approvalStart) >= approvalMetricCap {
		m.evictOldestApprovalLocked()
	}
	m.approvalSeq++
	m.approvalStart[key] = approvalMetric{start: time.Now(), runID: runID, seq: m.approvalSeq}
	if runID != "" {
		ids := m.approvalByRun[runID]
		if ids == nil {
			ids = map[string]struct{}{}
			m.approvalByRun[runID] = ids
		}
		ids[key] = struct{}{}
	}
	m.mu.Unlock()
}

// approvalAnswered records a decision and removes the pending entry.
func (m *metrics) approvalAnswered(runID, id string) {
	if id == "" {
		return
	}
	key := approvalKey(runID, id)
	m.mu.Lock()
	if metric, ok := m.approvalStart[key]; ok {
		delete(m.approvalStart, key)
		m.approvalSeconds += time.Since(metric.start).Seconds()
		m.approvals++
	}
	m.dropApprovalFromRunLocked(runID, key)
	m.mu.Unlock()
}

// approvalsEnded drops every pending approval for a finished, stopped or
// disconnected run. This covers timeouts and hosts that never answer.
func (m *metrics) approvalsEnded(runID string) {
	if runID == "" {
		return
	}
	m.mu.Lock()
	for key := range m.approvalByRun[runID] {
		delete(m.approvalStart, key)
	}
	delete(m.approvalByRun, runID)
	m.mu.Unlock()
}

func (m *metrics) dropApprovalFromRunLocked(runID, key string) {
	ids := m.approvalByRun[runID]
	if ids == nil {
		return
	}
	delete(ids, key)
	if len(ids) == 0 {
		delete(m.approvalByRun, runID)
	}
}

// evictOldestApprovalLocked is O(n) and only runs once the 10k cap is reached.
// That is cheap relative to the number of approvals and keeps the map bounded.
func (m *metrics) evictOldestApprovalLocked() {
	var oldestKey string
	var oldestSeq uint64
	for key, metric := range m.approvalStart {
		if oldestKey == "" || metric.seq < oldestSeq {
			oldestKey, oldestSeq = key, metric.seq
		}
	}
	if oldestKey == "" {
		return
	}
	metric := m.approvalStart[oldestKey]
	delete(m.approvalStart, oldestKey)
	m.dropApprovalFromRunLocked(metric.runID, oldestKey)
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
		requestID := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), ctxRequestID, requestID))
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
	counts, _ := s.store.DiagnosticsCounts()
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP nexus_hosts_connected Hosts with a live outbound link.\n# TYPE nexus_hosts_connected gauge\nnexus_hosts_connected %d\n", hosts)
	fmt.Fprintf(&b, "# HELP nexus_hosts_total Paired hosts that are not revoked.\n# TYPE nexus_hosts_total gauge\nnexus_hosts_total %d\n", counts.Hosts)
	fmt.Fprintf(&b, "# HELP nexus_sessions_total Native sessions known to the hub.\n# TYPE nexus_sessions_total gauge\nnexus_sessions_total %d\n", counts.Sessions)
	fmt.Fprintf(&b, "# HELP nexus_tasks_total Tasks stored on the hub.\n# TYPE nexus_tasks_total gauge\nnexus_tasks_total %d\n", counts.Tasks)
	fmt.Fprintf(&b, "# HELP nexus_users_total Hub user accounts.\n# TYPE nexus_users_total gauge\nnexus_users_total %d\n", counts.Users)
	fmt.Fprintf(&b, "# HELP nexus_queue_pending Queued prompts waiting for an idle run.\n# TYPE nexus_queue_pending gauge\nnexus_queue_pending %d\n", counts.QueuedPrompts)
	fmt.Fprintf(&b, "# HELP nexus_active_runs Runs without an end timestamp.\n# TYPE nexus_active_runs gauge\nnexus_active_runs %d\n", runs)
	fmt.Fprintf(&b, "# HELP nexus_browser_websockets Connected browser WebSockets.\n# TYPE nexus_browser_websockets gauge\nnexus_browser_websockets %d\n", clients)
	s.metrics.mu.Lock()
	fmt.Fprintf(&b, "# HELP nexus_host_reconnects_total Host links that replaced an older link.\n# TYPE nexus_host_reconnects_total counter\nnexus_host_reconnects_total %d\n", s.metrics.hostReconnects)
	fmt.Fprintf(&b, "# HELP nexus_push_failures_total Web Push delivery failures.\n# TYPE nexus_push_failures_total counter\nnexus_push_failures_total %d\n", s.metrics.pushFailures)
	fmt.Fprintf(&b, "# HELP nexus_agent_protocol_errors_total Structured-run errors reported by agents.\n# TYPE nexus_agent_protocol_errors_total counter\nnexus_agent_protocol_errors_total %d\n", s.metrics.protocolErrors)
	fmt.Fprintf(&b, "# HELP nexus_run_limit_rejections_total Runs rejected by a configured concurrency limit.\n# TYPE nexus_run_limit_rejections_total counter\nnexus_run_limit_rejections_total %d\n", s.metrics.runLimitRejects)
	fmt.Fprintf(&b, "# HELP nexus_approval_latency_seconds Time between approval request and decision.\n# TYPE nexus_approval_latency_seconds summary\n")
	fmt.Fprintf(&b, "nexus_approval_latency_seconds_sum %g\nnexus_approval_latency_seconds_count %d\n", s.metrics.approvalSeconds, s.metrics.approvals)
	fmt.Fprintf(&b, "# HELP nexus_approvals_pending Approvals waiting for a decision.\n# TYPE nexus_approvals_pending gauge\nnexus_approvals_pending %d\n", len(s.metrics.approvalStart))
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
