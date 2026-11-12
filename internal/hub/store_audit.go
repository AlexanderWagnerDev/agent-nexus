package hub

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) AppendAudit(actor, action string, details any) error {
	// The hash chain is only valid when the read of the previous hash and the
	// insert are one atomic operation. Without this lock two concurrent
	// requests can both extend the same predecessor and fork the chain.
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	b, _ := json.Marshal(details)
	now := time.Now().UTC()
	var prev string
	err := s.db.QueryRow(`SELECT hash FROM audit ORDER BY seq DESC LIMIT 1`).Scan(&prev)
	if errors.Is(err, sql.ErrNoRows) {
		prev = strings.Repeat("0", 64)
	} else if err != nil {
		return err
	}
	payload := fmt.Sprintf("%s|%s|%s|%s|%d", prev, actor, action, string(b), now.UnixMilli())
	sum := sha256.Sum256([]byte(payload))
	_, err = s.db.Exec(`INSERT INTO audit(at,actor,action,details,prev_hash,hash) VALUES(?,?,?,?,?,?)`,
		now.UnixMilli(), actor, action, string(b), prev, hex.EncodeToString(sum[:]))
	return err
}

func (s *Store) ListAudit(limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT seq,at,actor,action,details,prev_hash,hash FROM audit ORDER BY seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var at int64
		if err := rows.Scan(&e.Seq, &at, &e.Actor, &e.Action, &e.Details, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.At = fromMS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AuditForHost returns recent audit entries whose details mention the host.
// Host ids are random hex, so a LIKE pattern cannot be widened by caller input.
func (s *Store) AuditForHost(hostID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT seq,at,actor,action,details,prev_hash,hash FROM audit
		WHERE details LIKE ? ORDER BY seq DESC LIMIT ?`, "%"+hostID+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var at int64
		if err := rows.Scan(&e.Seq, &at, &e.Actor, &e.Action, &e.Details, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		e.At = fromMS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyAudit recomputes the hash chain. It returns the first broken sequence
// number, or 0 if the chain is intact.
func (s *Store) VerifyAudit() (int64, error) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	rows, err := s.db.Query(`SELECT seq,at,actor,action,details,prev_hash,hash FROM audit ORDER BY seq`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := strings.Repeat("0", 64)
	for rows.Next() {
		var e AuditEvent
		var at int64
		if err := rows.Scan(&e.Seq, &at, &e.Actor, &e.Action, &e.Details, &e.PrevHash, &e.Hash); err != nil {
			return 0, err
		}
		payload := fmt.Sprintf("%s|%s|%s|%s|%d", prev, e.Actor, e.Action, e.Details, at)
		sum := sha256.Sum256([]byte(payload))
		if e.PrevHash != prev || hex.EncodeToString(sum[:]) != e.Hash {
			return e.Seq, nil
		}
		prev = e.Hash
	}
	return 0, rows.Err()
}
