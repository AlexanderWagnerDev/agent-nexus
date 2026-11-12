package hub

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

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
	sealed, err := s.seal(u.TOTPSecret)
	if err != nil {
		return User{}, err
	}
	if _, err := s.db.Exec(`INSERT INTO users(id, username, password_hash, totp_secret, role, created_at) VALUES(?,?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, sealed, u.Role, ms(u.CreatedAt)); err != nil {
		return User{}, err
	}
	return u, nil
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
		return errors.New("secret box is not configured; refusing to leave secrets unencrypted")
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
		sealed, err := s.seal(update.secret)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE users SET totp_secret=? WHERE id=?`, sealed, update.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetTOTP(userID, secret string, enabled bool) error {
	sealed, err := s.seal(secret)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=? WHERE id=?`, sealed, boolInt(enabled), userID)
	return err
}

func (s *Store) seal(value string) (string, error) {
	if s.secrets == nil || value == "" {
		return value, nil
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

// RevokeOtherDevices signs out every device except the current one. The
// caller closes their WebSockets after the store change.
func (s *Store) RevokeOtherDevices(userID, currentDeviceID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM devices WHERE user_id=? AND id<>? AND revoked_at IS NULL`, userID, currentDeviceID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	now := ms(time.Now())
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE devices SET revoked_at=? WHERE id=? AND user_id=?`, now, id, userID); err != nil {
			return ids, err
		}
		_, _ = s.db.Exec(`DELETE FROM push_subscriptions WHERE device_id=?`, id)
		_, _ = s.db.Exec(`DELETE FROM auth_sessions WHERE device_id=?`, id)
	}
	return ids, nil
}

// PurgeExpiredSessions removes auth sessions past their expiry. It runs at hub
// start and never touches unexpired device sessions.
func (s *Store) PurgeExpiredSessions() int {
	res, err := s.db.Exec(`DELETE FROM auth_sessions WHERE expires_at<?`, ms(time.Now()))
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// ---- host grants ----

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
