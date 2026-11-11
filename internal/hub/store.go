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
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	_ "modernc.org/sqlite"
)

// Store is the hub's metadata database. Transcript content is never written
// here; only session metadata, runs, devices, hosts and the audit log.
type Store struct {
	db      *sql.DB
	auditMu sync.Mutex
	secrets *SecretBox
}

// Backup writes a consistent SQLite snapshot while the hub is running. The
// caller should copy secret.key alongside it; encrypted secrets are useless
// without that key.
func (s *Store) Backup(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("backup path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("backup file already exists: %s", abs)
	}
	_, err = s.db.Exec(`VACUUM INTO ?`, filepath.ToSlash(abs))
	return err
}

// SetSecretBox enables encryption of sensitive values at rest. Existing
// plaintext values remain readable and are encrypted on their next write.
func (s *Store) SetSecretBox(box *SecretBox) {
	s.secrets = box
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

// MarkStepUp extends the step-up window for one signed-in session.
func (s *Store) MarkStepUp(token string, window time.Duration) error {
	_, err := s.db.Exec(`UPDATE auth_sessions SET step_up_until=? WHERE token_hash=?`,
		ms(time.Now().Add(window)), hashToken(token))
	return err
}

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
  expires_at INTEGER NOT NULL,
  step_up_until INTEGER
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
  repo_root TEXT NOT NULL DEFAULT '',
  repo_url TEXT NOT NULL DEFAULT '',
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
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS host_grants (
  host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  permissions TEXT NOT NULL DEFAULT 'view',
  PRIMARY KEY (host_id, user_id)
);
CREATE TABLE IF NOT EXISTS device_pairings (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER
);
CREATE TABLE IF NOT EXISTS user_settings (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  theme TEXT NOT NULL DEFAULT 'system',
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS host_confirmations (
  host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
  sas TEXT NOT NULL,
  confirmed_at INTEGER
);
CREATE TABLE IF NOT EXISTS passkeys (
  credential_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  data TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used INTEGER
);
CREATE TABLE IF NOT EXISTS session_favorites (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, session_id)
);
CREATE INDEX IF NOT EXISTS session_favorites_user_idx ON session_favorites(user_id, created_at);
CREATE TABLE IF NOT EXISTS notifications (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  run_id TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  read_at INTEGER
);
CREATE INDEX IF NOT EXISTS notifications_user_idx ON notifications(user_id, created_at);
`)
	if err != nil {
		return err
	}
	// Existing databases predate column-level migrations; SQLite reports a
	// duplicate-column error which is safe to ignore.
	_, _ = s.db.Exec(`ALTER TABLE auth_sessions ADD COLUMN step_up_until INTEGER`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN repo_root TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN repo_url TEXT NOT NULL DEFAULT ''`)
	return nil
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
	return s.CreateUserWithRole(username, passwordHash, totpSecret, "owner")
}

func (s *Store) CreateUserWithRole(username, passwordHash, totpSecret, role string) (User, error) {
	u := User{
		ID:           newID(),
		Username:     username,
		PasswordHash: passwordHash,
		TOTPSecret:   totpSecret,
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.db.Exec(`INSERT INTO users(id, username, password_hash, totp_secret, role, created_at) VALUES(?,?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, s.seal(u.TOTPSecret), u.Role, ms(u.CreatedAt))
	return u, err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, password_hash, totp_secret, totp_enabled, role, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		u.TOTPSecret = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserRole(id, role string) error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return err
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
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &totp, &u.Role, &created); err != nil {
		return u, err
	}
	plain, err := s.open(u.TOTPSecret)
	if err != nil {
		return u, err
	}
	u.TOTPSecret = plain
	u.TOTPEnabled = totp != 0
	u.CreatedAt = fromMS(created)
	return u, nil
}

// MigrateSecrets encrypts legacy plaintext values in place. It is idempotent
// and must run after SetSecretBox.
func (s *Store) MigrateSecrets() error {
	if s.secrets == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT id, totp_secret FROM users WHERE totp_secret <> ''`)
	if err != nil {
		return err
	}
	type pending struct{ id, secret string }
	var updates []pending
	for rows.Next() {
		var id, secret string
		if err := rows.Scan(&id, &secret); err != nil {
			rows.Close()
			return err
		}
		if !strings.HasPrefix(secret, secretPrefix) {
			updates = append(updates, pending{id, secret})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := s.db.Exec(`UPDATE users SET totp_secret=? WHERE id=?`, s.seal(update.secret), update.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetTOTP(userID, secret string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=? WHERE id=?`, s.seal(secret), boolInt(enabled), userID)
	return err
}

func (s *Store) seal(value string) string {
	if s.secrets == nil || value == "" {
		return value
	}
	return s.secrets.Seal(value)
}

func (s *Store) open(value string) (string, error) {
	if s.secrets == nil || value == "" {
		return value, nil
	}
	return s.secrets.Open(value)
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
	User        User
	Device      Device
	ExpiresAt   time.Time
	StepUpUntil time.Time
}

func (s *Store) SessionByToken(token string) (SessionInfo, error) {
	var si SessionInfo
	var deviceID string
	var userCreated, devCreated, devSeen, authCreated, authSeen, expires int64
	var totp int
	var revoked, stepUp sql.NullInt64
	err := s.db.QueryRow(`SELECT u.id,u.username,u.password_hash,u.totp_secret,u.totp_enabled,u.role,u.created_at,
		d.id,d.name,d.created_at,d.last_seen,d.revoked_at,
		a.created_at,a.last_seen,a.expires_at,a.step_up_until
		FROM auth_sessions a
		JOIN users u ON u.id=a.user_id
		JOIN devices d ON d.id=a.device_id
		WHERE a.token_hash=?`, hashToken(token)).Scan(
		&si.User.ID, &si.User.Username, &si.User.PasswordHash, &si.User.TOTPSecret, &totp, &si.User.Role, &userCreated,
		&deviceID, &si.Device.Name, &devCreated, &devSeen, &revoked,
		&authCreated, &authSeen, &expires, &stepUp)
	if err != nil {
		return si, err
	}
	if revoked.Valid {
		return si, errors.New("device revoked")
	}
	if si.User.TOTPSecret, err = s.open(si.User.TOTPSecret); err != nil {
		return si, err
	}
	si.User.TOTPEnabled = totp != 0
	si.User.CreatedAt = fromMS(userCreated)
	si.Device.ID = deviceID
	si.Device.CreatedAt = fromMS(devCreated)
	si.Device.LastSeen = fromMS(devSeen)
	si.ExpiresAt = fromMS(expires)
	if stepUp.Valid {
		si.StepUpUntil = fromMS(stepUp.Int64)
	}
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
	_, _ = s.db.Exec(`DELETE FROM push_subscriptions WHERE device_id=?`, deviceID)
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE device_id=?`, deviceID)
	return err
}

// ---- host grants ----

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

func (s *Store) UserTheme(userID string) (string, error) {
	var theme string
	err := s.db.QueryRow(`SELECT theme FROM user_settings WHERE user_id=?`, userID).Scan(&theme)
	if errors.Is(err, sql.ErrNoRows) {
		return "system", nil
	}
	return theme, err
}

func (s *Store) SetUserTheme(userID, theme string) error {
	_, err := s.db.Exec(`INSERT INTO user_settings(user_id,theme,updated_at) VALUES(?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET theme=excluded.theme, updated_at=excluded.updated_at`,
		userID, theme, ms(time.Now()))
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

func sessionID(hostID, agent, nativeID string) string {
	sum := sha256.Sum256([]byte(hostID + "\x00" + agent + "\x00" + nativeID))
	return hex.EncodeToString(sum[:16])
}

// ReplaceHostSessions applies a full snapshot from one host. It returns the
// number of upserted and deleted records. A full snapshot is also the repair
// path after a reconnect because it removes rows the host no longer reports.
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
		if err := upsertSession(tx, hostID, sess); err != nil {
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

// ApplyHostSessionDelta applies changed sessions and explicit removals. It
// never deletes rows that are absent from the payload; only a full snapshot
// may do that.
func (s *Store) ApplyHostSessionDelta(hostID string, sessions []protocol.Session, removed []protocol.SessionKey) (upserted, deleted int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, sess := range sessions {
		if err := upsertSession(tx, hostID, sess); err != nil {
			return 0, 0, err
		}
		upserted++
	}
	for _, key := range removed {
		if key.Agent == "" || key.NativeID == "" {
			continue
		}
		res, err := tx.Exec(`DELETE FROM sessions WHERE host_id=? AND agent=? AND native_id=?`, hostID, key.Agent, key.NativeID)
		if err != nil {
			return 0, 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted++
		}
	}
	return upserted, deleted, tx.Commit()
}

func upsertSession(tx *sql.Tx, hostID string, sess protocol.Session) error {
	id := sessionID(hostID, sess.Agent, sess.NativeID)
	state := sess.State
	if state == "" {
		state = protocol.StateUnknown
	}
	_, err := tx.Exec(`INSERT INTO sessions(id,host_id,agent,native_id,title,cwd,git_branch,repo_root,repo_url,created_at,updated_at,message_count,state,active_elsewhere,warning)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(host_id,agent,native_id) DO UPDATE SET
		title=excluded.title, cwd=excluded.cwd, git_branch=excluded.git_branch,
		repo_root=excluded.repo_root, repo_url=excluded.repo_url,
		updated_at=excluded.updated_at, message_count=excluded.message_count,
		state=excluded.state, active_elsewhere=excluded.active_elsewhere, warning=excluded.warning`,
		id, hostID, sess.Agent, sess.NativeID, sess.Title, sess.Cwd, sess.GitBranch, sess.RepoRoot, sess.RepoURL,
		ms(sess.CreatedAt), ms(sess.UpdatedAt), sess.MessageCount, state, boolInt(sess.ActiveElsewhere), sess.Warning)
	return err
}

func (s *Store) ListSessions(userID, hostID, agent, project, state, query string, limit, offset int, all bool) ([]protocol.SessionView, error) {
	if limit <= 0 || limit > 2001 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE h.revoked_at IS NULL`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
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
	q += ` ORDER BY s.updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
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

func (s *Store) SessionByID(userID, id string, all bool) (protocol.SessionView, error) {
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE s.id=? AND h.revoked_at IS NULL`
	args := []any{id}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	return scanSessionView(s.db.QueryRow(q, args...))
}

func scanSessionView(row rowScanner) (protocol.SessionView, error) {
	var v protocol.SessionView
	var created, updated int64
	var active int
	err := row.Scan(&v.ID, &v.HostID, &v.HostName, &v.Agent, &v.NativeID, &v.Title, &v.Cwd, &v.GitBranch, &v.RepoRoot, &v.RepoURL,
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
	err := s.db.QueryRow(`SELECT r.id,r.session_id,r.host_id,COALESCE(h.name,''),COALESCE(s.title,''),COALESCE(s.agent,''),
		COALESCE(s.cwd,''),COALESCE(s.git_branch,''),r.mode,r.state,r.lease_device_id,r.created_at,r.ended_at,r.started_by
		FROM runs r
		LEFT JOIN sessions s ON s.id=r.session_id
		LEFT JOIN hosts h ON h.id=r.host_id
		WHERE r.id=?`, id).
		Scan(&r.ID, &r.SessionID, &r.HostID, &r.HostName, &r.SessionTitle, &r.Agent, &r.Cwd, &r.GitBranch, &r.Mode, &r.State, &r.LeaseDeviceID, &created, &ended, &startedBy)
	r.StartedAt = fromMS(created)
	if ended.Valid {
		t := fromMS(ended.Int64)
		r.EndedAt = &t
	}
	return r, startedBy, err
}

func (s *Store) ActiveRunForSession(sessionID string) (string, bool) {
	var id string
	if sessionID == "" {
		return "", false
	}
	if err := s.db.QueryRow(`SELECT id FROM runs WHERE session_id=? AND ended_at IS NULL ORDER BY created_at DESC LIMIT 1`, sessionID).Scan(&id); err != nil {
		return "", false
	}
	return id, true
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

// ActiveRunsForUser lists live runs visible through host ownership or a view
// grant. It is intentionally bounded: the dashboard only needs recent runs.
func (s *Store) ActiveRunsForUser(userID string, all bool) ([]protocol.RunView, error) {
	q := `SELECT r.id,r.session_id,r.host_id,COALESCE(h.name,''),COALESCE(s.title,''),
		COALESCE(s.agent,''),COALESCE(s.cwd,''),COALESCE(s.git_branch,''),r.mode,r.state,r.created_at
		FROM runs r
		JOIN hosts h ON h.id=r.host_id
		LEFT JOIN sessions s ON s.id=r.session_id
		WHERE r.ended_at IS NULL AND h.revoked_at IS NULL`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	q += ` ORDER BY r.created_at DESC LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.RunView
	for rows.Next() {
		var r protocol.RunView
		var created int64
		if err := rows.Scan(&r.ID, &r.SessionID, &r.HostID, &r.HostName, &r.SessionTitle, &r.Agent, &r.Cwd, &r.GitBranch, &r.Mode, &r.State, &created); err != nil {
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

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ---- settings and push ----

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

type PushSubscription struct {
	ID       string `json:"id"`
	UserID   string `json:"-"`
	DeviceID string `json:"-"`
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

func (s *Store) SavePushSubscription(userID, deviceID, endpoint, p256dh, auth string) error {
	_, err := s.db.Exec(`INSERT INTO push_subscriptions(id,user_id,device_id,endpoint,p256dh,auth,created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, device_id=excluded.device_id, p256dh=excluded.p256dh, auth=excluded.auth`,
		newID(), userID, deviceID, endpoint, p256dh, auth, ms(time.Now()))
	return err
}

func (s *Store) DeletePushSubscription(userID, endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE user_id=? AND endpoint=?`, userID, endpoint)
	return err
}

func (s *Store) PushSubscriptions(userID string) ([]PushSubscription, error) {
	rows, err := s.db.Query(`SELECT id,user_id,device_id,endpoint,p256dh,auth FROM push_subscriptions WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.DeviceID, &p.Endpoint, &p.P256DH, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePushSubscriptionByID(id string) {
	s.db.Exec(`DELETE FROM push_subscriptions WHERE id=?`, id)
}

// ---- passkeys ----

func (s *Store) SavePasskey(userID, name string, cred *webauthn.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	_, err = s.db.Exec(`INSERT INTO passkeys(credential_id,user_id,name,data,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(credential_id) DO UPDATE SET name=excluded.name, data=excluded.data`,
		id, userID, name, string(raw), ms(time.Now()))
	return err
}

func (s *Store) Passkeys(userID string) ([]webauthn.Credential, error) {
	rows, err := s.db.Query(`SELECT data FROM passkeys WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if json.Unmarshal([]byte(raw), &c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (s *Store) PasskeyCount(userID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM passkeys WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

func (s *Store) UpdatePasskey(cred *webauthn.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	_, err = s.db.Exec(`UPDATE passkeys SET data=?, last_used=? WHERE credential_id=?`, string(raw), ms(time.Now()), id)
	return err
}

// ---- device pairing ----

func (s *Store) CreateDevicePairing(userID, token string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO device_pairings(token_hash,user_id,created_at,expires_at) VALUES(?,?,?,?)`,
		hashToken(token), userID, ms(now), ms(now.Add(ttl)))
	return err
}

func (s *Store) ConsumeDevicePairing(token string) (string, error) {
	var userID string
	now := ms(time.Now())
	err := s.db.QueryRow(`UPDATE device_pairings SET used_at=? WHERE token_hash=? AND used_at IS NULL AND expires_at>? RETURNING user_id`,
		now, hashToken(token), now).Scan(&userID)
	return userID, err
}
