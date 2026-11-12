package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

var taskStatuses = map[string]bool{
	protocol.TaskDraft: true, protocol.TaskReady: true, protocol.TaskQueued: true,
	protocol.TaskRunning: true, protocol.TaskWaitingApproval: true, protocol.TaskBlocked: true,
	protocol.TaskCompleted: true, protocol.TaskFailed: true, protocol.TaskCancelled: true,
}

var taskPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

func validTaskStatus(value string) bool { return taskStatuses[value] }

func validTaskPriority(value string) bool { return taskPriorities[value] }

// TaskFilter narrows the task listing. Sorting is fixed to recent updates.
type TaskFilter struct {
	Status  string
	HostID  string
	Agent   string
	Project string
	Query   string
	Limit   int
	Offset  int
}

func (s *Store) CreateTask(t protocol.Task, dependsOn []string) (protocol.Task, error) {
	now := time.Now().UTC()
	t.ID = newID()
	t.CreatedAt, t.UpdatedAt = now, now
	tags, _ := json.Marshal(t.Tags)
	tx, err := s.db.Begin()
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO tasks(id,user_id,title,description,project,host_id,agent,model,mode,priority,status,tags,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.Title, t.Description, t.Project, t.HostID, t.Agent, t.Model, t.Mode,
		t.Priority, t.Status, string(tags), ms(now), ms(now)); err != nil {
		return t, err
	}
	for _, dep := range dependsOn {
		if dep == "" {
			continue
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id=? AND user_id=?`, dep, t.UserID).Scan(&exists); err != nil || exists == 0 {
			return t, errors.New("dependency not found")
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_dependencies(task_id,depends_on) VALUES(?,?)`, t.ID, dep); err != nil {
			return t, err
		}
	}
	if err := tx.Commit(); err != nil {
		return t, err
	}
	s.PropagateTaskReadiness()
	return s.TaskByID(t.ID)
}

func (s *Store) TaskByID(id string) (protocol.Task, error) {
	var t protocol.Task
	var tags string
	var created, updated int64
	var started, completed sql.NullInt64
	err := s.db.QueryRow(`SELECT t.id,t.user_id,t.title,t.description,t.project,t.host_id,t.agent,t.model,t.mode,
		t.priority,t.status,t.session_id,t.run_id,t.result,t.error,t.tags,t.created_at,t.updated_at,t.started_at,t.completed_at,
		COALESCE(h.name,'')
		FROM tasks t LEFT JOIN hosts h ON h.id=t.host_id WHERE t.id=?`, id).
		Scan(&t.ID, &t.UserID, &t.Title, &t.Description, &t.Project, &t.HostID, &t.Agent, &t.Model, &t.Mode,
			&t.Priority, &t.Status, &t.SessionID, &t.RunID, &t.Result, &t.Error, &tags, &created, &updated, &started, &completed, &t.HostName)
	if err != nil {
		return t, err
	}
	t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
	if started.Valid {
		v := fromMS(started.Int64)
		t.StartedAt = &v
	}
	if completed.Valid {
		v := fromMS(completed.Int64)
		t.CompletedAt = &v
	}
	_ = json.Unmarshal([]byte(tags), &t.Tags)
	deps, _ := s.TaskDependencies(t.ID)
	for _, dep := range deps {
		t.DependsOn = append(t.DependsOn, dep.ID)
	}
	return t, nil
}

// TaskByRunID maps a live run back to the task that delegated it.
func (s *Store) TaskByRunID(runID string) (protocol.Task, error) {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM tasks WHERE run_id=? ORDER BY updated_at DESC LIMIT 1`, runID).Scan(&id); err != nil {
		return protocol.Task{}, err
	}
	return s.TaskByID(id)
}

func (s *Store) ListTasks(userID string, all bool, f TaskFilter) ([]protocol.Task, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	q := `SELECT t.id,t.user_id,t.title,t.description,t.project,t.host_id,t.agent,t.model,t.mode,
		t.priority,t.status,t.session_id,t.run_id,t.result,t.error,t.tags,t.created_at,t.updated_at,t.started_at,t.completed_at,
		COALESCE(h.name,'')
		FROM tasks t LEFT JOIN hosts h ON h.id=t.host_id WHERE 1=1`
	args := []any{}
	if !all {
		q += ` AND t.user_id=?`
		args = append(args, userID)
	}
	if f.Status != "" {
		q += ` AND t.status=?`
		args = append(args, f.Status)
	}
	if f.HostID != "" {
		q += ` AND t.host_id=?`
		args = append(args, f.HostID)
	}
	if f.Agent != "" {
		q += ` AND t.agent=?`
		args = append(args, f.Agent)
	}
	if f.Project != "" {
		q += ` AND t.project=?`
		args = append(args, f.Project)
	}
	if f.Query != "" {
		q += ` AND (t.title LIKE ? OR t.description LIKE ? OR t.project LIKE ?)`
		like := "%" + f.Query + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY t.updated_at DESC, t.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var out []protocol.Task
	for rows.Next() {
		var t protocol.Task
		var tags string
		var created, updated int64
		var started, completed sql.NullInt64
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Description, &t.Project, &t.HostID, &t.Agent, &t.Model, &t.Mode,
			&t.Priority, &t.Status, &t.SessionID, &t.RunID, &t.Result, &t.Error, &tags, &created, &updated, &started, &completed, &t.HostName); err != nil {
			rows.Close()
			return nil, err
		}
		t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
		if started.Valid {
			v := fromMS(started.Int64)
			t.StartedAt = &v
		}
		if completed.Valid {
			v := fromMS(completed.Int64)
			t.CompletedAt = &v
		}
		_ = json.Unmarshal([]byte(tags), &t.Tags)
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		deps, _ := s.TaskDependencies(out[i].ID)
		for _, dep := range deps {
			out[i].DependsOn = append(out[i].DependsOn, dep.ID)
		}
	}
	return out, nil
}

// TaskPatch updates only fields that were explicitly supplied.
type TaskPatch struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Project     *string   `json:"project"`
	HostID      *string   `json:"host_id"`
	Agent       *string   `json:"agent"`
	Model       *string   `json:"model"`
	Mode        *string   `json:"mode"`
	Priority    *string   `json:"priority"`
	Status      *string   `json:"status"`
	Result      *string   `json:"result"`
	Error       *string   `json:"error"`
	Tags        *[]string `json:"tags"`
}

func (s *Store) UpdateTask(id string, p TaskPatch) (protocol.Task, error) {
	sets := []string{}
	args := []any{}
	add := func(column string, value any) {
		sets = append(sets, column+"=?")
		args = append(args, value)
	}
	if p.Title != nil {
		add("title", *p.Title)
	}
	if p.Description != nil {
		add("description", *p.Description)
	}
	if p.Project != nil {
		add("project", *p.Project)
	}
	if p.HostID != nil {
		add("host_id", *p.HostID)
	}
	if p.Agent != nil {
		add("agent", *p.Agent)
	}
	if p.Model != nil {
		add("model", *p.Model)
	}
	if p.Mode != nil {
		add("mode", *p.Mode)
	}
	if p.Priority != nil {
		add("priority", *p.Priority)
	}
	if p.Status != nil {
		add("status", *p.Status)
		if *p.Status == protocol.TaskRunning {
			add("started_at", ms(time.Now()))
		}
		if *p.Status == protocol.TaskCompleted || *p.Status == protocol.TaskFailed || *p.Status == protocol.TaskCancelled {
			add("completed_at", ms(time.Now()))
		}
	}
	if p.Result != nil {
		add("result", *p.Result)
	}
	if p.Error != nil {
		add("error", *p.Error)
	}
	if p.Tags != nil {
		raw, _ := json.Marshal(*p.Tags)
		add("tags", string(raw))
	}
	if len(sets) == 0 {
		return s.TaskByID(id)
	}
	add("updated_at", ms(time.Now()))
	args = append(args, id)
	if _, err := s.db.Exec(`UPDATE tasks SET `+strings.Join(sets, ",")+` WHERE id=?`, args...); err != nil {
		return protocol.Task{}, err
	}
	s.PropagateTaskReadiness()
	return s.TaskByID(id)
}

func (s *Store) SetTaskStatus(id, status, result, taskErr string) error {
	now := ms(time.Now())
	var completed any
	if status == protocol.TaskCompleted || status == protocol.TaskFailed || status == protocol.TaskCancelled {
		completed = now
	}
	if _, err := s.db.Exec(`UPDATE tasks SET status=?, result=?, error=?, updated_at=?, completed_at=? WHERE id=?`,
		status, result, taskErr, now, completed, id); err != nil {
		return err
	}
	s.PropagateTaskReadiness()
	return nil
}

func (s *Store) DeleteTask(id string) error {
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) TaskDependencies(taskID string) ([]protocol.TaskRef, error) {
	rows, err := s.db.Query(`SELECT d.depends_on,t.title,t.status FROM task_dependencies d
		JOIN tasks t ON t.id=d.depends_on WHERE d.task_id=? ORDER BY t.created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.TaskRef
	for rows.Next() {
		var r protocol.TaskRef
		if err := rows.Scan(&r.ID, &r.Title, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) TaskDependents(taskID string) ([]protocol.TaskRef, error) {
	rows, err := s.db.Query(`SELECT t.id,t.title,t.status FROM task_dependencies d
		JOIN tasks t ON t.id=d.task_id WHERE d.depends_on=? ORDER BY t.created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.TaskRef
	for rows.Next() {
		var r protocol.TaskRef
		if err := rows.Scan(&r.ID, &r.Title, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddTaskDependency rejects a dependency that would create a cycle. The
// recursive query follows existing dependency edges from the proposed parent.
func (s *Store) AddTaskDependency(taskID, dependsOn string) error {
	if taskID == dependsOn {
		return errors.New("a task cannot depend on itself")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id IN (?,?)`, taskID, dependsOn).Scan(&exists); err != nil || exists != 2 {
		return errors.New("task not found")
	}
	var cycle int
	if err := s.db.QueryRow(`WITH RECURSIVE chain(id) AS (
		SELECT depends_on FROM task_dependencies WHERE task_id=?
		UNION
		SELECT d.depends_on FROM task_dependencies d JOIN chain c ON d.task_id=c.id
	) SELECT COUNT(*) FROM chain WHERE id=?`, dependsOn, taskID).Scan(&cycle); err != nil {
		return err
	}
	if cycle > 0 {
		return errors.New("dependency would create a cycle")
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO task_dependencies(task_id,depends_on) VALUES(?,?)`, taskID, dependsOn); err != nil {
		return err
	}
	s.PropagateTaskReadiness()
	return nil
}

func (s *Store) RemoveTaskDependency(taskID, dependsOn string) error {
	if _, err := s.db.Exec(`DELETE FROM task_dependencies WHERE task_id=? AND depends_on=?`, taskID, dependsOn); err != nil {
		return err
	}
	s.PropagateTaskReadiness()
	return nil
}

// PropagateTaskReadiness moves tasks between blocked and ready when their
// dependencies change. Runtime-owned states (running, waiting, terminal) are
// never rewritten.
func (s *Store) PropagateTaskReadiness() {
	rows, err := s.db.Query(`SELECT t.id,t.status,
		(SELECT COUNT(*) FROM task_dependencies d WHERE d.task_id=t.id),
		(SELECT COUNT(*) FROM task_dependencies d JOIN tasks dep ON dep.id=d.depends_on WHERE d.task_id=t.id AND dep.status='completed'),
		(SELECT COUNT(*) FROM task_dependencies d JOIN tasks dep ON dep.id=d.depends_on WHERE d.task_id=t.id AND dep.status IN ('failed','cancelled'))
		FROM tasks t
		WHERE t.status IN ('draft','ready','blocked')
		LIMIT 1000`)
	if err != nil {
		return
	}
	type change struct {
		id, status, message string
	}
	var changes []change
	for rows.Next() {
		var id, status string
		var total, done, bad int
		if err := rows.Scan(&id, &status, &total, &done, &bad); err != nil {
			continue
		}
		switch {
		case bad > 0 && status != protocol.TaskBlocked:
			changes = append(changes, change{id, protocol.TaskBlocked, "a dependency failed or was cancelled"})
		case total > 0 && done < total && status != protocol.TaskBlocked:
			changes = append(changes, change{id, protocol.TaskBlocked, "waiting for dependencies"})
		case total > 0 && done == total && status != protocol.TaskReady:
			changes = append(changes, change{id, protocol.TaskReady, ""})
		case total == 0 && status == protocol.TaskBlocked:
			changes = append(changes, change{id, protocol.TaskReady, ""})
		}
	}
	rows.Close()
	now := ms(time.Now())
	for _, c := range changes {
		_, _ = s.db.Exec(`UPDATE tasks SET status=?, error=?, updated_at=? WHERE id=?`, c.status, c.message, now, c.id)
	}
}

// SyncTaskRunState mirrors run lifecycle changes onto the delegating task.
func (s *Store) SyncTaskRunState(runID, runState string) {
	task, err := s.TaskByRunID(runID)
	if err != nil || task.Status == protocol.TaskCancelled || task.Status == protocol.TaskCompleted {
		return
	}
	switch runState {
	case protocol.StateStarting, protocol.StateRunning:
		if task.Status != protocol.TaskWaitingApproval {
			_ = s.SetTaskStatus(task.ID, protocol.TaskRunning, task.Result, "")
		}
	case protocol.StateWaiting:
		if task.Status != protocol.TaskWaitingApproval {
			_ = s.SetTaskStatus(task.ID, protocol.TaskWaitingApproval, task.Result, "")
		}
	case protocol.StateCompleted, protocol.StateExited:
		if task.Status != protocol.TaskCompleted {
			result := task.Result
			if strings.TrimSpace(result) == "" {
				result = "run completed"
			}
			_ = s.SetTaskStatus(task.ID, protocol.TaskCompleted, result, "")
		}
	case protocol.StateFailed:
		if task.Status != protocol.TaskFailed {
			_ = s.SetTaskStatus(task.ID, protocol.TaskFailed, task.Result, "run failed")
		}
	}
}

// ReconcileTasks runs at hub start. A task whose run is gone can no longer be
// trusted to be running; leave tasks with a live run untouched so a
// reconnecting host can continue them.
func (s *Store) ReconcileTasks() int {
	now := ms(time.Now())
	res, err := s.db.Exec(`UPDATE tasks SET status='failed', error=?, updated_at=?, completed_at=?
		WHERE status IN ('running','waiting_for_approval')
		AND (run_id='' OR run_id NOT IN (SELECT id FROM runs WHERE ended_at IS NULL))`,
		"hub restarted before the run finished", now, now)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	s.PropagateTaskReadiness()
	return int(n)
}

// LinkTaskRun attaches a delegated run and records the native session once it
// is known.
func (s *Store) LinkTaskRun(taskID, runID, sessionID string) error {
	_, err := s.db.Exec(`UPDATE tasks SET run_id=?, session_id=COALESCE(NULLIF(?,''),session_id), status=?, error='', updated_at=?, started_at=COALESCE(started_at,?) WHERE id=?`,
		runID, sessionID, protocol.TaskRunning, ms(time.Now()), ms(time.Now()), taskID)
	return err
}

// AttachTaskSession updates the session id after a structured new/fork run
// reported its native session.
func (s *Store) AttachTaskSession(runID, sessionID string) {
	_, _ = s.db.Exec(`UPDATE tasks SET session_id=?, updated_at=? WHERE run_id=?`, sessionID, ms(time.Now()), runID)
}

func (s *Store) CountTasks(userID string, all bool) (int, error) {
	q := `SELECT COUNT(*) FROM tasks`
	args := []any{}
	if !all {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (s *Store) TaskOwnedBy(taskID, userID string) (protocol.Task, error) {
	t, err := s.TaskByID(taskID)
	if err != nil {
		return t, err
	}
	if t.UserID != userID {
		return t, fmt.Errorf("task belongs to another user")
	}
	return t, nil
}
