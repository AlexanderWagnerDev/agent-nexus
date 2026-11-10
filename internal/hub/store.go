package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	_ "modernc.org/sqlite"
)

// Store is the hub's metadata database. Transcript content is never written
// here; only session metadata, runs, devices, hosts and the audit log.
type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  role TEXT NOT NULL DEFAULT 'owner',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  revoked_at INTEGER
);
CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pairing_codes (
  code_hash TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER
);
CREATE TABLE IF NOT EXISTS hosts (
  id TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  public_key BLOB NOT NULL,
  os TEXT NOT NULL DEFAULT '',
  arch TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  policy TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  revoked_at INTEGER
);
CREATE TABLE IF NOT EXISTS host_agents (
  host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  path TEXT NOT NULL,
  supported INTEGER NOT NULL,
  capabilities TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (host_id, name)
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  agent TEXT NOT NULL,
  native_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  cwd TEXT NOT NULL DEFAULT '',
  git_branch TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'unknown',
  active_elsewhere INTEGER NOT NULL DEFAULT 0,
  warning TEXT NOT NULL DEFAULT '',
  UNIQUE (host_id, agent, native_id)
);
CREATE INDEX IF NOT EXISTS sessions_host_idx ON sessions(host_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS sessions_updated_idx ON sessions(updated_at DESC);
CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL DEFAULT '',
  host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  mode TEXT NOT NULL,
  state TEXT NOT NULL,
  started_by TEXT NOT NULL,
  lease_device_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  ended_at INTEGER
);
CREATE INDEX IF NOT EXISTS runs_host_idx ON runs(host_id, state);
CREATE TABLE IF NOT EXISTS audit (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  at INTEGER NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  details TEXT NOT NULL,
  prev_hash TEXT NOT NULL,
  hash TEXT NOT NULL
);
`)
	return err
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func ms(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

// ---- users ----

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	TOTPSecret   string    `json:"-"`
	TOTPEnabled  bool      `json:"totp_enabled"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) UserCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash, totpSecret string) (User, error) {
	u := User{
		ID:           newID(),
		Username:     username,
		PasswordHash: passwordHash,
		TOTPSecret:   totpSecret,
		Role:         "owner",
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.db.Exec(`INSERT INTO users(id, username, password_hash, totp_secret, created_at) VALUES(?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, u.TOTPSecret, ms(u.CreatedAt))
	return u, err
}

func (s *Store) UserByUsername(username string) (User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, username, password_hash, totp_secret, totp_enabled, role, created_at FROM users WHERE username=?`, username))
}

func (s *Store) UserByID(id string) (User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, username, password_hash, totp_secret, totp_enabled, role, created_at FROM users WHERE id=?`, id))
}

type rowScanner interface{ Scan(...any) error }

func (s *Store) scanUser(row rowScanner) (User, error) {
	var u User
	var created int64
	var totp int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &totp, &u.Role, &created)
	u.TOTPEnabled = totp != 0
	u.CreatedAt = fromMS(created)
	return u, err
}

func (s *Store) SetTOTP(userID, secret string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=? WHERE id=?`, secret, boolInt(enabled), userID)
	return err
}

// ---- devices and browser sessions ----

type Device struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  time.Time  `json:"last_seen"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) CreateSession(userID, deviceName string) (token string, device Device, err error) {
	now := time.Now().UTC()
	device = Device{ID: newID(), Name: deviceName, CreatedAt: now, LastSeen: now}
	token = randomToken(32)
	_, err = s.db.Exec(`INSERT INTO devices(id, user_id, name, created_at, last_seen) VALUES(?,?,?,?,?)`,
		device.ID, userID, device.Name, ms(now), ms(now))
	if err != nil {
		return "", Device{}, err
	}
	_, err = s.db.Exec(`INSERT INTO auth_sessions(token_hash, user_id, device_id, created_at, last_seen, expires_at) VALUES(?,?,?,?,?,?)`,
		hashToken(token), userID, device.ID, ms(now), ms(now), ms(now.Add(30*24*time.Hour)))
	return token, device, err
}

type SessionInfo struct {
	User      User
	Device    Device
	ExpiresAt time.Time
}

func (s *Store) SessionByToken(token string) (SessionInfo, error) {
	var si SessionInfo
	var deviceID string
	var userCreated, devCreated, devSeen, authCreated, authSeen, expires int64
	var totp int
	var revoked sql.NullInt64
	err := s.db.QueryRow(`SELECT u.id,u.username,u.password_hash,u.totp_secret,u.totp_enabled,u.role,u.created_at,
		d.id,d.name,d.created_at,d.last_seen,d.revoked_at,
		a.created_at,a.last_seen,a.expires_at
		FROM auth_sessions a
		JOIN users u ON u.id=a.user_id
		JOIN devices d ON d.id=a.device_id
		WHERE a.token_hash=?`, hashToken(token)).Scan(
		&si.User.ID, &si.User.Username, &si.User.PasswordHash, &si.User.TOTPSecret, &totp, &si.User.Role, &userCreated,
		&deviceID, &si.Device.Name, &devCreated, &devSeen, &revoked,
		&authCreated, &authSeen, &expires)
	if err != nil {
		return si, err
	}
	if revoked.Valid {
		return si, errors.New("device revoked")
	}
	si.User.TOTPEnabled = totp != 0
	si.User.CreatedAt = fromMS(userCreated)
	si.Device.ID = deviceID
	si.Device.CreatedAt = fromMS(devCreated)
	si.Device.LastSeen = fromMS(devSeen)
	si.ExpiresAt = fromMS(expires)
	if time.Now().After(si.ExpiresAt) {
		return si, errors.New("session expired")
	}
	return si, nil
}

func (s *Store) TouchSession(token, deviceID string) {
	now := ms(time.Now())
	s.db.Exec(`UPDATE auth_sessions SET last_seen=? WHERE token_hash=?`, now, hashToken(token))
	s.db.Exec(`UPDATE devices SET last_seen=? WHERE id=?`, now, deviceID)
}

func (s *Store) RevokeSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE token_hash=?`, hashToken(token))
	return err
}

func (s *Store) ListDevices(userID string) ([]Device, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at,last_seen,revoked_at FROM devices WHERE user_id=? ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var created, seen int64
		var revoked sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Name, &created, &seen, &revoked); err != nil {
			return nil, err
		}
		d.CreatedAt, d.LastSeen = fromMS(created), fromMS(seen)
		if revoked.Valid {
			t := fromMS(revoked.Int64)
			d.RevokedAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) RevokeDevice(userID, deviceID string) error {
	now := ms(time.Now())
	if _, err := s.db.Exec(`UPDATE devices SET revoked_at=? WHERE id=? AND user_id=?`, now, deviceID, userID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE device_id=?`, deviceID)
	return err
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

func (s *Store) HostPublicKey(hostID string) ([]byte, error) {
	var key []byte
	var revoked sql.NullInt64
	err := s.db.QueryRow(`SELECT public_key, revoked_at FROM hosts WHERE id=?`, hostID).Scan(&key, &revoked)
	if revoked.Valid {
		return nil, errors.New("host revoked")
	}
	return key, err
}

func (s *Store) ListHosts(ownerID string) ([]protocol.HostView, error) {
	rows, err := s.db.Query(`SELECT id,name,os,arch,version,last_seen,policy FROM hosts WHERE owner_id=? AND revoked_at IS NULL ORDER BY name`, ownerID)
	if err != nil {
		return nil, err
	}
	var out []protocol.HostView
	for rows.Next() {
		var h protocol.HostView
		var seen int64
		var policyJSON string
		if err := rows.Scan(&h.ID, &h.Name, &h.OS, &h.Arch, &h.Version, &seen, &policyJSON); err != nil {
			rows.Close()
			return nil, err
		}
		h.LastSeen = fromMS(seen)
		h.Status = "offline"
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

// ---- sessions ----

func sessionID(hostID, agent, nativeID string) string {
	sum := sha256.Sum256([]byte(hostID + "\x00" + agent + "\x00" + nativeID))
	return hex.EncodeToString(sum[:16])
}

// ReplaceHostSessions applies a full snapshot from one host. It returns the
// number of upserted and deleted records.
func (s *Store) ReplaceHostSessions(hostID string, sessions []protocol.Session) (upserted, deleted int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	keep := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		id := sessionID(hostID, sess.Agent, sess.NativeID)
		keep[id] = true
		state := sess.State
		if state == "" {
			state = protocol.StateUnknown
		}
		_, err := tx.Exec(`INSERT INTO sessions(id,host_id,agent,native_id,title,cwd,git_branch,created_at,updated_at,message_count,state,active_elsewhere,warning)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(host_id,agent,native_id) DO UPDATE SET
			title=excluded.title, cwd=excluded.cwd, git_branch=excluded.git_branch,
			updated_at=excluded.updated_at, message_count=excluded.message_count,
			state=excluded.state, active_elsewhere=excluded.active_elsewhere, warning=excluded.warning`,
			id, hostID, sess.Agent, sess.NativeID, sess.Title, sess.Cwd, sess.GitBranch,
			ms(sess.CreatedAt), ms(sess.UpdatedAt), sess.MessageCount, state, boolInt(sess.ActiveElsewhere), sess.Warning)
		if err != nil {
			return 0, 0, err
		}
		upserted++
	}
	rows, err := tx.Query(`SELECT id FROM sessions WHERE host_id=?`, hostID)
	if err != nil {
		return 0, 0, err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && !keep[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE id=?`, id); err != nil {
			return 0, 0, err
		}
		deleted++
	}
	return upserted, deleted, tx.Commit()
}

func (s *Store) ListSessions(ownerID, hostID, agent, project, state, query string, limit int) ([]protocol.SessionView, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE h.owner_id=? AND h.revoked_at IS NULL`
	args := []any{ownerID}
	if hostID != "" {
		q += ` AND s.host_id=?`
		args = append(args, hostID)
	}
	if agent != "" {
		q += ` AND s.agent=?`
		args = append(args, agent)
	}
	if project != "" {
		q += ` AND s.cwd LIKE ?`
		args = append(args, "%"+project+"%")
	}
	if state != "" {
		q += ` AND s.state=?`
		args = append(args, state)
	}
	if query != "" {
		q += ` AND (s.title LIKE ? OR s.cwd LIKE ?)`
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	q += ` ORDER BY s.updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.SessionView
	for rows.Next() {
		v, err := scanSessionView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SessionIDForNative maps a host-local native session to its hub ID.
func (s *Store) SessionIDForNative(hostID, agent, nativeID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM sessions WHERE host_id=? AND agent=? AND native_id=?`, hostID, agent, nativeID).Scan(&id)
	return id, err
}

func (s *Store) SessionByID(ownerID, id string) (protocol.SessionView, error) {
	row := s.db.QueryRow(`SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE s.id=? AND h.owner_id=?`, id, ownerID)
	return scanSessionView(row)
}

func scanSessionView(row rowScanner) (protocol.SessionView, error) {
	var v protocol.SessionView
	var created, updated int64
	var active int
	err := row.Scan(&v.ID, &v.HostID, &v.HostName, &v.Agent, &v.NativeID, &v.Title, &v.Cwd, &v.GitBranch,
		&created, &updated, &v.MessageCount, &v.State, &active, &v.Warning)
	v.CreatedAt, v.UpdatedAt = fromMS(created), fromMS(updated)
	v.ActiveElsewhere = active != 0
	return v, err
}

// SetSessionState updates the live state of a session.
func (s *Store) SetSessionState(id, state string) {
	s.db.Exec(`UPDATE sessions SET state=? WHERE id=?`, state, id)
}

// ---- runs ----

func (s *Store) CreateRun(id, sessionID, hostID, mode, userID, deviceID string) error {
	_, err := s.db.Exec(`INSERT INTO runs(id,session_id,host_id,mode,state,started_by,lease_device_id,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, sessionID, hostID, mode, protocol.StateStarting, userID, deviceID, ms(time.Now()))
	return err
}

func (s *Store) RunByID(id string) (protocol.RunView, string, error) {
	var r protocol.RunView
	var created int64
	var ended sql.NullInt64
	var startedBy string
	err := s.db.QueryRow(`SELECT id,session_id,host_id,mode,state,lease_device_id,created_at,ended_at,started_by FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.SessionID, &r.HostID, &r.Mode, &r.State, &r.LeaseDeviceID, &created, &ended, &startedBy)
	r.StartedAt = fromMS(created)
	if ended.Valid {
		t := fromMS(ended.Int64)
		r.EndedAt = &t
	}
	return r, startedBy, err
}

func (s *Store) UpdateRunState(id, state string) {
	if state == protocol.StateExited || state == protocol.StateFailed || state == protocol.StateCompleted {
		s.db.Exec(`UPDATE runs SET state=?, ended_at=? WHERE id=?`, state, ms(time.Now()), id)
		return
	}
	s.db.Exec(`UPDATE runs SET state=? WHERE id=?`, state, id)
}

func (s *Store) ActiveRuns(hostID string) ([]protocol.RunView, error) {
	rows, err := s.db.Query(`SELECT id,session_id,host_id,mode,state,lease_device_id,created_at FROM runs WHERE host_id=? AND ended_at IS NULL`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.RunView
	for rows.Next() {
		var r protocol.RunView
		var created int64
		if err := rows.Scan(&r.ID, &r.SessionID, &r.HostID, &r.Mode, &r.State, &r.LeaseDeviceID, &created); err != nil {
			return nil, err
		}
		r.StartedAt = fromMS(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- audit ----

type AuditEvent struct {
	Seq      int64     `json:"seq"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Details  string    `json:"details"`
	PrevHash string    `json:"prev_hash"`
	Hash     string    `json:"hash"`
}

func (s *Store) AppendAudit(actor, action string, details any) error {
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

// VerifyAudit recomputes the hash chain. It returns the first broken sequence
// number, or 0 if the chain is intact.
func (s *Store) VerifyAudit() (int64, error) {
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

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
