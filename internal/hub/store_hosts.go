package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Store) HostGrantPermissions(hostID, userID string) (string, error) {
	var perms string
	err := s.db.QueryRow(`SELECT permissions FROM host_grants WHERE host_id=? AND user_id=?`, hostID, userID).Scan(&perms)
	return perms, err
}

func (s *Store) SetHostGrant(hostID, userID, permissions string) error {
	_, err := s.db.Exec(`INSERT INTO host_grants(host_id,user_id,permissions) VALUES(?,?,?)
		ON CONFLICT(host_id,user_id) DO UPDATE SET permissions=excluded.permissions`, hostID, userID, permissions)
	return err
}

func (s *Store) DeleteHostGrant(hostID, userID string) error {
	_, err := s.db.Exec(`DELETE FROM host_grants WHERE host_id=? AND user_id=?`, hostID, userID)
	return err
}

type HostGrant struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Permissions string `json:"permissions"`
}

func (s *Store) ListHostGrants(hostID string) ([]HostGrant, error) {
	rows, err := s.db.Query(`SELECT g.user_id,u.username,g.permissions
		FROM host_grants g JOIN users u ON u.id=g.user_id WHERE g.host_id=? ORDER BY u.username`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostGrant
	for rows.Next() {
		var g HostGrant
		if err := rows.Scan(&g.UserID, &g.Username, &g.Permissions); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---- pairing ----

func (s *Store) CreatePairingCode(ownerID, code string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO pairing_codes(code_hash, owner_id, created_at, expires_at) VALUES(?,?,?,?)`,
		hashToken(code), ownerID, ms(now), ms(now.Add(ttl)))
	return err
}

func (s *Store) ConsumePairingCode(code string) (ownerID string, err error) {
	now := ms(time.Now())
	err = s.db.QueryRow(`UPDATE pairing_codes SET used_at=? WHERE code_hash=? AND used_at IS NULL AND expires_at>? RETURNING owner_id`,
		now, hashToken(code), now).Scan(&ownerID)
	return ownerID, err
}

// ---- hosts ----

type Host struct {
	protocol.HostView
	OwnerID   string
	PublicKey []byte
	RevokedAt *time.Time
	Policy    *protocol.PolicySummary
	CreatedAt time.Time
}

func (s *Store) CreateHost(ownerID, name string, publicKey []byte, osName, arch, version string) (string, error) {
	id := newID()
	now := time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO hosts(id, owner_id, name, public_key, os, arch, version, created_at, last_seen) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, ownerID, name, publicKey, osName, arch, version, ms(now), ms(now))
	return id, err
}

func (s *Store) CreateHostConfirmation(hostID, sas string) error {
	_, err := s.db.Exec(`INSERT INTO host_confirmations(host_id,sas) VALUES(?,?)
		ON CONFLICT(host_id) DO UPDATE SET sas=excluded.sas, confirmed_at=NULL`, hostID, sas)
	return err
}

func (s *Store) HostConfirmation(hostID string) (sas string, confirmed bool, exists bool, err error) {
	var at sql.NullInt64
	err = s.db.QueryRow(`SELECT sas,confirmed_at FROM host_confirmations WHERE host_id=?`, hostID).Scan(&sas, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", true, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return sas, at.Valid && at.Int64 > 0, true, nil
}

func (s *Store) ConfirmHost(ownerID, hostID string) error {
	res, err := s.db.Exec(`UPDATE host_confirmations SET confirmed_at=?
		WHERE host_id=? AND host_id IN (SELECT id FROM hosts WHERE owner_id=? AND revoked_at IS NULL)`,
		ms(time.Now()), hostID, ownerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) HostPublicKey(hostID string) ([]byte, error) {
	var key []byte
	var revoked sql.NullInt64
	err := s.db.QueryRow(`SELECT public_key, revoked_at FROM hosts WHERE id=?`, hostID).Scan(&key, &revoked)
	if revoked.Valid {
		return nil, errors.New("host revoked")
	}
	return key, err
}

func (s *Store) ListHosts(userID string, all bool) ([]protocol.HostView, error) {
	q := `SELECT h.id,h.name,h.os,h.arch,h.version,h.last_seen,h.policy,
		COALESCE(c.sas,''), c.confirmed_at FROM hosts h
		LEFT JOIN host_confirmations c ON c.host_id=h.id WHERE h.revoked_at IS NULL`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	q += ` ORDER BY h.name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var out []protocol.HostView
	for rows.Next() {
		var h protocol.HostView
		var seen int64
		var policyJSON, sas string
		var confirmed sql.NullInt64
		if err := rows.Scan(&h.ID, &h.Name, &h.OS, &h.Arch, &h.Version, &seen, &policyJSON, &sas, &confirmed); err != nil {
			rows.Close()
			return nil, err
		}
		h.LastSeen = fromMS(seen)
		h.Status = "offline"
		h.SAS = sas
		h.Pending = sas != "" && (!confirmed.Valid || confirmed.Int64 == 0)
		h.Agents = []protocol.AgentInfo{}
		if policyJSON != "" && policyJSON != "{}" {
			var p protocol.PolicySummary
			if json.Unmarshal([]byte(policyJSON), &p) == nil {
				h.Policy = &p
			}
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Fetch agents only after the host cursor is closed: the store uses one
	// SQLite connection, and nested queries on a live cursor would deadlock.
	for i := range out {
		out[i].Agents, _ = s.HostAgents(out[i].ID)
	}
	return out, nil
}

func (s *Store) HostByID(hostID string) (Host, error) {
	var h Host
	var seen, created int64
	var revoked sql.NullInt64
	var policyJSON string
	err := s.db.QueryRow(`SELECT id,owner_id,name,public_key,os,arch,version,policy,created_at,last_seen,revoked_at FROM hosts WHERE id=?`, hostID).
		Scan(&h.ID, &h.OwnerID, &h.Name, &h.PublicKey, &h.OS, &h.Arch, &h.Version, &policyJSON, &created, &seen, &revoked)
	if err != nil {
		return h, err
	}
	h.CreatedAt = fromMS(created)
	h.LastSeen = fromMS(seen)
	h.Status = "offline"
	if policyJSON != "" && policyJSON != "{}" {
		var p protocol.PolicySummary
		if json.Unmarshal([]byte(policyJSON), &p) == nil {
			h.Policy = &p
		}
	}
	if revoked.Valid {
		t := fromMS(revoked.Int64)
		h.RevokedAt = &t
	}
	return h, nil
}

func (s *Store) UpdateHostSeen(hostID string) {
	s.db.Exec(`UPDATE hosts SET last_seen=? WHERE id=?`, ms(time.Now()), hostID)
}

func (s *Store) SetHostInventory(hostID, version string, agents []protocol.AgentInfo, policy *protocol.PolicySummary) error {
	policyJSON, _ := json.Marshal(policy)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE hosts SET version=?, policy=?, last_seen=? WHERE id=?`, version, string(policyJSON), ms(time.Now()), hostID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM host_agents WHERE host_id=?`, hostID); err != nil {
		return err
	}
	for _, a := range agents {
		caps, _ := json.Marshal(a.Capabilities)
		if _, err := tx.Exec(`INSERT INTO host_agents(host_id,name,version,path,supported,capabilities) VALUES(?,?,?,?,?,?)`,
			hostID, a.Name, a.Version, a.Path, boolInt(a.Supported), string(caps)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) HostAgents(hostID string) ([]protocol.AgentInfo, error) {
	rows, err := s.db.Query(`SELECT name,version,path,supported,capabilities FROM host_agents WHERE host_id=? ORDER BY name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.AgentInfo
	for rows.Next() {
		var a protocol.AgentInfo
		var supported int
		var caps string
		if err := rows.Scan(&a.Name, &a.Version, &a.Path, &supported, &caps); err != nil {
			return nil, err
		}
		a.Supported = supported != 0
		_ = json.Unmarshal([]byte(caps), &a.Capabilities)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RevokeHost(ownerID, hostID string) error {
	_, err := s.db.Exec(`UPDATE hosts SET revoked_at=? WHERE id=? AND owner_id=?`, ms(time.Now()), hostID, ownerID)
	return err
}

func (s *Store) RevokeHostByID(hostID string) error {
	_, err := s.db.Exec(`UPDATE hosts SET revoked_at=? WHERE id=?`, ms(time.Now()), hostID)
	return err
}

// ---- sessions ----
