package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
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

func sessionID(hostID, agent, nativeID string) string {
	sum := sha256.Sum256([]byte(hostID + "\x00" + agent + "\x00" + nativeID))
	return hex.EncodeToString(sum[:16])
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
