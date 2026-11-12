package hub

import (
	"net/http"
	"strings"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type taskCreateRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Project     string   `json:"project"`
	HostID      string   `json:"host_id"`
	Agent       string   `json:"agent"`
	Model       string   `json:"model"`
	Mode        string   `json:"mode"`
	Priority    string   `json:"priority"`
	Status      string   `json:"status"`
	Tags        []string `json:"tags"`
	DependsOn   []string `json:"depends_on"`
}

type taskDelegateRequest struct {
	Prompt    string `json:"prompt"`
	Mode      string `json:"mode"` // new, resume, fork
	SessionID string `json:"session_id"`
	RunMode   string `json:"run_mode"` // structured only for delegated prompts
	Model     string `json:"model"`
	AgentMode string `json:"agent_mode"`
}

func (s *Server) taskVisible(u User, task protocol.Task) bool {
	return task.UserID == u.ID || isAdminRole(u.Role)
}

func (s *Server) loadTask(w http.ResponseWriter, r *http.Request) (User, protocol.Task, bool) {
	u := userFrom(r)
	task, err := s.store.TaskByID(r.PathValue("id"))
	if err != nil || !s.taskVisible(u, task) {
		writeError(w, http.StatusNotFound, "task not found")
		return u, task, false
	}
	return u, task, true
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()
	limit, offset := atoiOr(q.Get("limit"), 200), atoiOr(q.Get("offset"), 0)
	if limit < 1 || limit > 500 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	tasks, err := s.store.ListTasks(u.ID, isAdminRole(u.Role), TaskFilter{
		Status: strings.TrimSpace(q.Get("status")), HostID: strings.TrimSpace(q.Get("host")),
		Agent: strings.TrimSpace(q.Get("agent")), Project: strings.TrimSpace(q.Get("project")),
		Query: strings.TrimSpace(q.Get("q")), Limit: limit, Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []protocol.Task{}
	}
	total, _ := s.store.CountTasks(u.ID, isAdminRole(u.Role))
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "total": total})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req taskCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		writeError(w, http.StatusBadRequest, "title must be 1-200 characters")
		return
	}
	if len(req.Description) > 64<<10 {
		writeError(w, http.StatusBadRequest, "description is too large")
		return
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "normal"
	}
	if !validTaskPriority(priority) {
		writeError(w, http.StatusBadRequest, "priority must be low, normal, high or urgent")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = protocol.TaskDraft
	}
	if !validTaskStatus(status) || status == protocol.TaskRunning || status == protocol.TaskWaitingApproval {
		writeError(w, http.StatusBadRequest, "invalid initial task status")
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = protocol.ModeStructured
	}
	if mode != protocol.ModeStructured && mode != protocol.ModeTerminal {
		writeError(w, http.StatusBadRequest, "unknown task mode")
		return
	}
	hostID := strings.TrimSpace(req.HostID)
	if hostID != "" && !s.hostPermission(u, hostID, "view") {
		writeError(w, http.StatusForbidden, "host view permission required")
		return
	}
	if status == protocol.TaskReady {
		// A ready task with unmet dependencies is immediately blocked by the
		// store; this is a state, not an error.
	}
	task := protocol.Task{
		UserID: u.ID, Title: title, Description: strings.TrimSpace(req.Description),
		Project: strings.TrimSpace(req.Project), HostID: hostID, Agent: strings.TrimSpace(req.Agent),
		Model: strings.TrimSpace(req.Model), Mode: mode, Priority: priority, Status: status,
		Tags: req.Tags,
	}
	created, err := s.store.CreateTask(task, req.DependsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(u.Username, "task.created", map[string]string{"task_id": created.ID, "host_id": created.HostID})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	deps, _ := s.store.TaskDependencies(task.ID)
	dependents, _ := s.store.TaskDependents(task.ID)
	payload := map[string]any{"task": task, "dependencies": deps, "dependents": dependents}
	if task.RunID != "" {
		if run, _, err := s.store.RunByID(task.RunID); err == nil {
			payload["run"] = run
		}
	}
	if task.SessionID != "" {
		if sess, err := s.store.SessionByID(u.ID, task.SessionID, isAdminRole(u.Role)); err == nil {
			payload["session"] = sess
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	var req TaskPatch
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || len(title) > 200 {
			writeError(w, http.StatusBadRequest, "title must be 1-200 characters")
			return
		}
		req.Title = &title
	}
	if req.Priority != nil && !validTaskPriority(*req.Priority) {
		writeError(w, http.StatusBadRequest, "priority must be low, normal, high or urgent")
		return
	}
	if req.Mode != nil && *req.Mode != protocol.ModeStructured && *req.Mode != protocol.ModeTerminal {
		writeError(w, http.StatusBadRequest, "unknown task mode")
		return
	}
	if req.HostID != nil && *req.HostID != "" && !s.hostPermission(u, *req.HostID, "view") {
		writeError(w, http.StatusForbidden, "host view permission required")
		return
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validTaskStatus(status) {
			writeError(w, http.StatusBadRequest, "unknown task status")
			return
		}
		if status == protocol.TaskRunning || status == protocol.TaskWaitingApproval {
			writeError(w, http.StatusConflict, "running and waiting states are managed by the linked run")
			return
		}
		if task.RunID != "" {
			if _, active := s.store.ActiveRunForSession(task.SessionID); active && status != protocol.TaskCancelled {
				writeError(w, http.StatusConflict, "stop the active run before changing the task status")
				return
			}
		}
		req.Status = &status
	}
	updated, err := s.store.UpdateTask(task.ID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "task.updated", map[string]string{"task_id": task.ID, "status": updated.Status})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	if task.RunID != "" {
		if _, active := s.store.ActiveRunForSession(task.SessionID); active {
			writeError(w, http.StatusConflict, "stop the active run before deleting the task")
			return
		}
	}
	if err := s.store.DeleteTask(task.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "task.deleted", map[string]string{"task_id": task.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleTaskDependency(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	var req struct {
		DependsOn string `json:"depends_on"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.DependsOn) == "" {
		writeError(w, http.StatusBadRequest, "depends_on is required")
		return
	}
	dep, err := s.store.TaskByID(req.DependsOn)
	if err != nil || dep.UserID != task.UserID {
		writeError(w, http.StatusNotFound, "dependency not found")
		return
	}
	if err := s.store.AddTaskDependency(task.ID, dep.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(u.Username, "task.dependency_added", map[string]string{"task_id": task.ID, "depends_on": dep.ID})
	updated, _ := s.store.TaskByID(task.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTaskDependency(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	dep := r.PathValue("dep")
	if err := s.store.RemoveTaskDependency(task.ID, dep); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "task.dependency_removed", map[string]string{"task_id": task.ID, "depends_on": dep})
	updated, _ := s.store.TaskByID(task.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDelegateTask(w http.ResponseWriter, r *http.Request) {
	u, task, ok := s.loadTask(w, r)
	if !ok {
		return
	}
	var req taskDelegateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if task.Status == protocol.TaskRunning || task.Status == protocol.TaskWaitingApproval {
		writeError(w, http.StatusConflict, "task is already running")
		return
	}
	if task.Status == protocol.TaskCompleted || task.Status == protocol.TaskCancelled {
		writeError(w, http.StatusConflict, "task is closed")
		return
	}
	deps, _ := s.store.TaskDependencies(task.ID)
	for _, dep := range deps {
		if dep.Status != protocol.TaskCompleted {
			writeError(w, http.StatusConflict, "task has unfinished dependencies")
			return
		}
	}
	action := strings.TrimSpace(req.Mode)
	if action == "" {
		action = protocol.ActionNew
	}
	if action != protocol.ActionNew && action != protocol.ActionResume && action != protocol.ActionFork {
		writeError(w, http.StatusBadRequest, "mode must be new, resume or fork")
		return
	}
	runMode := strings.TrimSpace(req.RunMode)
	if runMode == "" {
		runMode = protocol.ModeStructured
	}
	if runMode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "delegated tasks require structured mode so the prompt can be delivered")
		return
	}
	hostID := task.HostID
	agent := task.Agent
	cwd := task.Project
	nativeID := ""
	sessionID := ""
	if action == protocol.ActionNew {
		if hostID == "" || cwd == "" || agent == "" {
			writeError(w, http.StatusBadRequest, "task needs host, project and agent for a new session")
			return
		}
		if !s.hostPermission(u, hostID, "create") {
			writeError(w, http.StatusForbidden, "create permission required")
			return
		}
	} else {
		sessionID = strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			sessionID = task.SessionID
		}
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "a session is required for resume or fork")
			return
		}
		sess, err := s.store.SessionByID(u.ID, sessionID, isAdminRole(u.Role))
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		need := "resume"
		if action == protocol.ActionFork {
			need = "fork"
		}
		if !s.sessionPermission(u, sess, need) {
			writeError(w, http.StatusForbidden, need+" permission required")
			return
		}
		hostID, agent, cwd, nativeID = sess.HostID, sess.Agent, sess.Cwd, sess.NativeID
	}
	host, err := s.store.HostByID(hostID)
	if err != nil || host.RevokedAt != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	if host.DisabledAt != nil {
		writeError(w, http.StatusConflict, "host is disabled")
		return
	}
	if !s.hostOnline(hostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Description)
	}
	if prompt == "" {
		prompt = task.Title
	}
	if len(prompt) > maxPromptBytes {
		writeError(w, http.StatusBadRequest, "prompt is too large")
		return
	}
	sess := protocol.SessionView{HostID: hostID, Agent: agent, Cwd: cwd, NativeID: nativeID, ID: sessionID}
	run, err := s.startRun(u, deviceFrom(r), sess, action, cwd, runMode, 120, 30, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.store.LinkTaskRun(task.ID, run.ID, sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	options := &protocol.PromptOptions{Model: req.Model, AgentMode: req.AgentMode}
	if options.Model == "" && options.AgentMode == "" {
		options = nil
	}
	if _, err := s.store.EnqueuePrompt(run.ID, u.ID, deviceFrom(r).ID, prompt, options, nil); err != nil {
		s.store.SetTaskStatus(task.ID, protocol.TaskFailed, "", "cannot queue the delegated prompt: "+err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.armQueue(run.ID)
	s.kickQueue()
	s.audit(u.Username, "task.delegated", map[string]string{"task_id": task.ID, "run_id": run.ID, "host_id": hostID, "action": action})
	updated, _ := s.store.TaskByID(task.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"task": updated, "run": run})
}
