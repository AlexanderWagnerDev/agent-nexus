package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// EnqueuePrompt stores one prompt at the end of a run's queue. Options may
// contain bounded attachments; they are stored as JSON only on the hub.
func (s *Store) EnqueuePrompt(runID, userID, deviceID, text string, options *protocol.PromptOptions, scheduledAt *time.Time) (protocol.PromptQueueItem, error) {
	rawOptions := ""
	if options != nil {
		raw, err := json.Marshal(options)
		if err != nil {
			return protocol.PromptQueueItem{}, err
		}
		rawOptions = string(raw)
	}
	var scheduled any
	if scheduledAt != nil {
		scheduled = ms(*scheduledAt)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.PromptQueueItem{}, err
	}
	defer tx.Rollback()
	var position int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM prompt_queue WHERE run_id=?`, runID).Scan(&position); err != nil {
		return protocol.PromptQueueItem{}, err
	}
	id := newID()
	now := ms(time.Now())
	if _, err := tx.Exec(`INSERT INTO prompt_queue(id,run_id,user_id,device_id,position,text,options,scheduled_at,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,'queued',?,?)`,
		id, runID, userID, deviceID, position, text, rawOptions, scheduled, now, now); err != nil {
		return protocol.PromptQueueItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.PromptQueueItem{}, err
	}
	return s.PromptQueueItem(id)
}

func (s *Store) PromptQueueItem(id string) (protocol.PromptQueueItem, error) {
	row := s.db.QueryRow(`SELECT id,run_id,user_id,device_id,position,text,options,scheduled_at,state,attempts,last_error,created_at,updated_at
		FROM prompt_queue WHERE id=?`, id)
	return scanPromptQueueItem(row)
}

type queueRow interface {
	Scan(dest ...any) error
}

func scanPromptQueueItem(row queueRow) (protocol.PromptQueueItem, error) {
	var item protocol.PromptQueueItem
	var options string
	var scheduled sql.NullInt64
	var created, updated int64
	if err := row.Scan(&item.ID, &item.RunID, &item.UserID, &item.DeviceID, &item.Position, &item.Text,
		&options, &scheduled, &item.State, &item.Attempts, &item.LastError, &created, &updated); err != nil {
		return item, err
	}
	if options != "" {
		var parsed protocol.PromptOptions
		if json.Unmarshal([]byte(options), &parsed) == nil {
			item.Options = &parsed
		}
	}
	if scheduled.Valid {
		t := fromMS(scheduled.Int64)
		item.ScheduledAt = &t
	}
	item.CreatedAt, item.UpdatedAt = fromMS(created), fromMS(updated)
	return item, nil
}

// ListPromptQueue returns the pending and failed items for a run.
func (s *Store) ListPromptQueue(runID string) ([]protocol.PromptQueueItem, error) {
	rows, err := s.db.Query(`SELECT id,run_id,user_id,device_id,position,text,options,scheduled_at,state,attempts,last_error,created_at,updated_at
		FROM prompt_queue WHERE run_id=? AND state IN ('queued','sending','failed') ORDER BY position, created_at LIMIT 200`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.PromptQueueItem{}
	for rows.Next() {
		item, err := scanPromptQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePromptQueueItem(runID, itemID, text string, scheduledAt *time.Time) error {
	var scheduled any
	if scheduledAt != nil {
		scheduled = ms(*scheduledAt)
	}
	res, err := s.db.Exec(`UPDATE prompt_queue SET text=?, scheduled_at=?, updated_at=? WHERE id=? AND run_id=? AND state='queued'`,
		text, scheduled, ms(time.Now()), itemID, runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("queue item is no longer editable")
	}
	return nil
}

func (s *Store) DeletePromptQueueItem(runID, itemID string) error {
	res, err := s.db.Exec(`DELETE FROM prompt_queue WHERE id=? AND run_id=? AND state!='sending'`, itemID, runID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("queue item is not removable")
	}
	return nil
}

// ReorderPromptQueue applies a complete new order of the queued items.
func (s *Store) ReorderPromptQueue(runID string, ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM prompt_queue WHERE run_id=? AND state='queued'`, runID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		have[id] = true
		count++
	}
	rows.Close()
	if count != len(ids) {
		return errors.New("reorder must include every queued item")
	}
	seen := map[string]bool{}
	for position, id := range ids {
		if !have[id] || seen[id] {
			return errors.New("reorder contains an unknown or duplicate item")
		}
		seen[id] = true
		if _, err := tx.Exec(`UPDATE prompt_queue SET position=?, updated_at=? WHERE id=? AND run_id=?`,
			position+1, ms(time.Now()), id, runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetPromptQueuePaused(runID, userID string, paused bool) error {
	res, err := s.db.Exec(`UPDATE runs SET queue_paused=? WHERE id=? AND started_by=?`, boolInt(paused), runID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("run not found")
	}
	return nil
}

func (s *Store) PromptQueuePaused(runID string) (bool, error) {
	var paused int
	err := s.db.QueryRow(`SELECT queue_paused FROM runs WHERE id=?`, runID).Scan(&paused)
	return paused != 0, err
}

// DuePromptQueue returns queued items whose schedule has arrived.
func (s *Store) DuePromptQueue(now time.Time) ([]protocol.PromptQueueItem, error) {
	rows, err := s.db.Query(`SELECT id,run_id,user_id,device_id,position,text,options,scheduled_at,state,attempts,last_error,created_at,updated_at
		FROM prompt_queue WHERE state='queued' AND (scheduled_at IS NULL OR scheduled_at<=?)
		ORDER BY run_id, position, created_at LIMIT 500`, ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.PromptQueueItem
	for rows.Next() {
		item, err := scanPromptQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ClaimPromptQueueItem atomically moves one queued item to sending. Only the
// goroutine that wins this update may deliver it to the host.
func (s *Store) ClaimPromptQueueItem(id string) (protocol.PromptQueueItem, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.PromptQueueItem{}, false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE prompt_queue SET state='sending', attempts=attempts+1, updated_at=? WHERE id=? AND state='queued'`,
		ms(time.Now()), id)
	if err != nil {
		return protocol.PromptQueueItem{}, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return protocol.PromptQueueItem{}, false, nil
	}
	row := tx.QueryRow(`SELECT id,run_id,user_id,device_id,position,text,options,scheduled_at,state,attempts,last_error,created_at,updated_at
		FROM prompt_queue WHERE id=?`, id)
	item, err := scanPromptQueueItem(row)
	if err != nil {
		return protocol.PromptQueueItem{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.PromptQueueItem{}, false, err
	}
	return item, true, nil
}

func (s *Store) FinishPromptQueueItem(id, state, lastError string) {
	_, _ = s.db.Exec(`UPDATE prompt_queue SET state=?, last_error=?, updated_at=? WHERE id=?`,
		state, lastError, ms(time.Now()), id)
}

// FailPendingPromptQueue marks every queued or in-flight item of a finished
// run. In-flight items are never retried automatically.
func (s *Store) FailPendingPromptQueue(runID, reason string) {
	_, _ = s.db.Exec(`UPDATE prompt_queue SET state='failed', last_error=?, updated_at=? WHERE run_id=? AND state IN ('queued','sending')`,
		reason, ms(time.Now()), runID)
}

// HasQueuedPrompts reports whether any queue item survived a restart.
func (s *Store) HasQueuedPrompts() (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM prompt_queue WHERE state IN ('queued','sending')`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// HasQueuedPromptsForRun avoids waking the queue worker on every idle event.
func (s *Store) HasQueuedPromptsForRun(runID string) bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM prompt_queue WHERE run_id=? AND state='queued'`, runID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// RecoverPromptQueue runs at hub start. A crash can leave an item in sending;
// it is ambiguous whether the host received it, so it must not be resent.
func (s *Store) RecoverPromptQueue() {
	_, _ = s.db.Exec(`UPDATE prompt_queue SET state='failed', last_error=?, updated_at=? WHERE state='sending'`,
		"hub restarted while the prompt was being sent; it was not retried automatically", ms(time.Now()))
	_, _ = s.db.Exec(`DELETE FROM prompt_queue WHERE state='sent' AND updated_at<?`, ms(time.Now().Add(-24*time.Hour)))
}
