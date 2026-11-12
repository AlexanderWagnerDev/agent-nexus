package hub

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type fanoutTarget struct {
	HostID string `json:"host_id"`
	Agent  string `json:"agent"`
	Cwd    string `json:"cwd"`
}

type fanoutRequest struct {
	Prompt  string         `json:"prompt"`
	Mode    string         `json:"mode"`
	Targets []fanoutTarget `json:"targets"`
}

func (s *Server) handleCreateFanout(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req fanoutRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := validQueuedPrompt(req.Prompt, nil); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeStructured
	}
	if req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "fan-out requires structured mode so the prompt can be delivered safely")
		return
	}
	if len(req.Targets) == 0 || len(req.Targets) > 8 {
		writeError(w, http.StatusBadRequest, "fan-out requires 1 to 8 targets")
		return
	}
	seen := map[string]bool{}
	for i := range req.Targets {
		target := &req.Targets[i]
		target.HostID = strings.TrimSpace(target.HostID)
		target.Agent = strings.TrimSpace(target.Agent)
		target.Cwd = strings.TrimSpace(target.Cwd)
		if target.HostID == "" || target.Agent == "" || target.Cwd == "" {
			writeError(w, http.StatusBadRequest, "every target needs host_id, agent and cwd")
			return
		}
		if !strings.HasPrefix(target.Cwd, "/") && !strings.Contains(target.Cwd, ":") {
			writeError(w, http.StatusBadRequest, "cwd must be an absolute path")
			return
		}
		key := target.HostID + "\x00" + target.Cwd
		if seen[key] {
			writeError(w, http.StatusBadRequest, "two targets would write to the same worktree")
			return
		}
		seen[key] = true
		host, err := s.store.HostByID(target.HostID)
		if err != nil || host.RevokedAt != nil {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		if host.DisabledAt != nil {
			writeError(w, http.StatusConflict, "host is disabled")
			return
		}
		if !s.hostPermission(u, target.HostID, "create") {
			writeError(w, http.StatusForbidden, "create permission required for every target host")
			return
		}
		if !s.hostOnline(target.HostID) {
			writeError(w, http.StatusServiceUnavailable, "target host is offline: "+target.HostID)
			return
		}
	}

	fanoutID, err := s.store.CreateFanout(u.ID, strings.TrimSpace(req.Prompt))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	device := deviceFrom(r)
	errors := []string{}
	for _, target := range req.Targets {
		run, err := s.startRun(u, device, protocol.SessionView{
			HostID: target.HostID, Agent: target.Agent, Cwd: target.Cwd,
		}, protocol.ActionNew, target.Cwd, protocol.ModeStructured, 120, 32, false)
		if err != nil {
			errors = append(errors, target.HostID+": "+err.Error())
			continue
		}
		if _, err := s.store.EnqueuePrompt(run.ID, u.ID, device.ID, strings.TrimSpace(req.Prompt), nil, nil); err != nil {
			_ = s.sendHost(target.HostID, protocol.Envelope{Type: "run.stop", RunID: run.ID})
			s.setRunState(run.ID, protocol.StateFailed)
			_ = s.store.AddFanoutRun(fanoutID, run.ID, target.HostID, target.Agent, "cannot queue prompt: "+err.Error())
			errors = append(errors, target.HostID+": "+err.Error())
			continue
		}
		_ = s.store.AddFanoutRun(fanoutID, run.ID, target.HostID, target.Agent, "")
	}
	s.audit(u.Username, "fanout.created", map[string]string{"fanout_id": fanoutID, "targets": strconv.Itoa(len(req.Targets))})
	s.broadcast(protocol.ServerEnvelope{Type: "fanout.changed"})
	fanout, _ := s.store.Fanout(fanoutID, u.ID, isAdminRole(u.Role))
	writeJSON(w, http.StatusCreated, map[string]any{"fanout": fanout, "errors": errors})
}

func (s *Server) handleListFanouts(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	items, err := s.store.ListFanouts(u.ID, isAdminRole(u.Role), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []FanoutView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fanouts": items})
}

func (s *Server) handleGetFanout(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	view, err := s.store.Fanout(r.PathValue("id"), u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "fan-out not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}
