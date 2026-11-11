package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()
	limit := atoiOr(q.Get("limit"), 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	offset := atoiOr(q.Get("offset"), 0)
	sessions, err := s.store.ListSessions(u.ID, q.Get("host"), q.Get("agent"), q.Get("project"), q.Get("state"), q.Get("q"), limit+1, offset, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	if sessions == nil {
		sessions = []protocol.SessionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "has_more": hasMore, "offset": offset})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	offset := atoiOr(r.URL.Query().Get("offset"), 0)
	limit := atoiOr(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	params, _ := json.Marshal(map[string]any{"native_id": sess.NativeID, "agent": sess.Agent, "offset": offset, "limit": limit})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.hostRequest(ctx, sess.HostID, "transcript", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var tr protocol.Transcript
	if err := json.Unmarshal(result, &tr); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleSessionGit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]string{"cwd": sess.Cwd})
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, sess.HostID, "git", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 || len(query) > 200 {
		writeError(w, http.StatusBadRequest, "query must be 2-200 characters")
		return
	}
	hosts, err := s.store.ListHosts(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type result struct {
		hits   []protocol.SearchHit
		host   string
		hostID string
	}
	ch := make(chan result, len(hosts))
	online := 0
	for _, h := range hosts {
		if !s.hostPermission(u, h.ID, "read_transcript") {
			continue
		}
		if !s.hostOnline(h.ID) {
			continue
		}
		online++
		go func(hostID, hostName string) {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{"query": query, "limit": 50})
			raw, err := s.hostRequest(ctx, hostID, "search", params)
			if err != nil {
				ch <- result{host: hostName, hostID: hostID}
				return
			}
			var hits []protocol.SearchHit
			_ = json.Unmarshal(raw, &hits)
			ch <- result{hits: hits, host: hostName, hostID: hostID}
		}(h.ID, h.Name)
	}
	all := []protocol.SearchHit{}
	seen := map[string]bool{}
	search := map[string]bool{}
	for i := 0; i < online; i++ {
		res := <-ch
		if len(res.hits) > 0 {
			for _, hit := range res.hits {
				key := res.hostID + "|" + hit.Agent + "|" + hit.NativeID
				if seen[key] {
					continue
				}
				seen[key] = true
				hit.HostID = res.hostID
				hit.HostName = res.host
				hit.Title = displayTitle(hit.Title)
				hit.Snippet = strings.TrimSpace(hit.Snippet)
				if id, err := s.store.SessionIDForNative(res.hostID, hit.Agent, hit.NativeID); err == nil {
					hit.SessionID = id
				}
				all = append(all, hit)
			}
		} else {
			search[res.host] = true
		}
	}
	if len(all) > 100 {
		all = all[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": all, "not_searched": search, "hosts_total": len(hosts)})
}

// ---- runs ----

type runRequest struct {
	HostID string `json:"host_id"`
	Agent  string `json:"agent"`
	Cwd    string `json:"cwd"`
	Mode   string `json:"mode"`
	Fork   bool   `json:"fork"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "resume") {
		writeError(w, http.StatusForbidden, "resume permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	if runID, active := s.store.ActiveRunForSession(sess.ID); active {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "session is already active", "run_id": runID,
		})
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Fork && !s.sessionPermission(u, sess, "fork") {
		writeError(w, http.StatusForbidden, "fork permission required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	action := protocol.ActionResume
	if req.Fork {
		action = protocol.ActionFork
	}
	run, err := s.startRun(u, deviceFrom(r), sess, action, req.Cwd, req.Mode, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleNewRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	host, err := s.store.HostByID(hostID)
	if err != nil || host.RevokedAt != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	if !s.hostPermission(u, hostID, "create") {
		writeError(w, http.StatusForbidden, "create permission required")
		return
	}
	if !s.hostOnline(hostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Agent == "" || req.Cwd == "" {
		writeError(w, http.StatusBadRequest, "agent and cwd are required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	run, err := s.startRun(u, deviceFrom(r), protocol.SessionView{HostID: hostID, Agent: req.Agent}, protocol.ActionNew, req.Cwd, req.Mode, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) startRun(u User, d Device, sess protocol.SessionView, action, cwd, mode string, cols, rows int) (protocol.RunView, error) {
	id := newID()
	if cwd == "" {
		cwd = sess.Cwd
	}
	if cwd == "" && action != protocol.ActionNew {
		return protocol.RunView{}, errors.New("session has no working directory")
	}
	if err := s.store.CreateRun(id, sess.ID, sess.HostID, mode, u.ID, d.ID); err != nil {
		return protocol.RunView{}, err
	}
	spec := protocol.RunSpec{
		RunID: id, SessionID: sess.ID, Agent: sess.Agent, NativeID: sess.NativeID,
		Cwd: cwd, Action: action, Mode: mode, Cols: cols, Rows: rows,
	}
	s.mu.Lock()
	s.runHosts[id] = sess.HostID
	s.mu.Unlock()
	if err := s.sendHost(sess.HostID, protocol.Envelope{Type: "run.start", Run: &spec}); err != nil {
		s.store.UpdateRunState(id, protocol.StateFailed)
		return protocol.RunView{}, err
	}
	s.setRunState(id, protocol.StateStarting)
	s.audit(u.Username, "run.started", map[string]string{"run_id": id, "host_id": sess.HostID, "agent": sess.Agent, "action": action, "mode": mode})
	run, _, _ := s.store.RunByID(id)
	return run, nil
}

func (s *Server) handleActiveRuns(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	runs, err := s.store.ActiveRunsForUser(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []protocol.RunView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	run.LastEventSeq = s.runEventCursor(run.ID)
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRunOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	if !s.hostOnline(run.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]string{"run_id": run.ID})
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, run.HostID, "run.options", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var options protocol.RunOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.stop", RunID: run.ID}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.setRunState(run.ID, protocol.StateStopping)
	s.audit(u.Username, "run.stopped", map[string]string{"run_id": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	d := deviceFrom(r)
	run.LeaseDeviceID = d.ID
	_, err = s.store.db.Exec(`UPDATE runs SET lease_device_id=? WHERE id=?`, d.ID, run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.broadcast(protocol.ServerEnvelope{Type: "run.lease", RunID: run.ID, Message: d.Name})
	s.audit(u.Username, "run.lease_acquired", map[string]string{"run_id": run.ID, "device_id": d.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "lease_acquired"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role != "owner" && u.Role != "admin" {
		writeError(w, http.StatusForbidden, "owner role required")
		return
	}
	events, err := s.store.ListAudit(atoiOr(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	broken, _ := s.store.VerifyAudit()
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "broken_seq": broken})
}
