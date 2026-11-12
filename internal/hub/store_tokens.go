package hub

import (
	"database/sql"
	"time"
)

type APIToken struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

func (s *Store) CreateAPIToken(userID, name string) (APIToken, string, error) {
	raw := randomToken(32)
	now := ms(time.Now())
	id := newID()
	_, err := s.db.Exec(`INSERT INTO api_tokens(id,user_id,name,token_hash,scopes,created_at) VALUES(?,?,?,?,?,?)`,
		id, userID, name, hashToken(raw), "read", now)
	if err != nil {
		return APIToken{}, "", err
	}
	return APIToken{ID: id, Name: name, Scopes: []string{"read"}, CreatedAt: fromMS(now)}, raw, nil
}

func (s *Store) ListAPITokens(userID string) ([]APIToken, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at,last_used FROM api_tokens WHERE user_id=? AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var token APIToken
		var created int64
		var last sql.NullInt64
		if err := rows.Scan(&token.ID, &token.Name, &created, &last); err != nil {
			return nil, err
		}
		token.Scopes = []string{"read"}
		token.CreatedAt = fromMS(created)
		if last.Valid {
			t := fromMS(last.Int64)
			token.LastUsed = &t
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func (s *Store) RevokeAPIToken(userID, id string) error {
	res, err := s.db.Exec(`UPDATE api_tokens SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, ms(time.Now()), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// APITokenUser validates a bearer token and returns the owner. The scope set
// is deliberately fixed to read-only for this version.
func (s *Store) APITokenUser(raw string) (User, string, bool) {
	if raw == "" {
		return User{}, "", false
	}
	var userID, tokenID string
	err := s.db.QueryRow(`SELECT id,user_id FROM api_tokens WHERE token_hash=? AND revoked_at IS NULL`, hashToken(raw)).Scan(&tokenID, &userID)
	if err != nil {
		return User{}, "", false
	}
	user, err := s.UserByID(userID)
	if err != nil {
		return User{}, "", false
	}
	_, _ = s.db.Exec(`UPDATE api_tokens SET last_used=? WHERE id=?`, ms(time.Now()), tokenID)
	return user, tokenID, true
}
