package hub

import (
	"database/sql"
	"strings"
	"time"
)

type PromptTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	Shared    bool      `json:"shared"`
	Owner     bool      `json:"owner"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) ListPromptTemplates(userID string) ([]PromptTemplate, error) {
	rows, err := s.db.Query(`SELECT id,user_id,name,body,shared,updated_at FROM prompt_templates
		WHERE user_id=? OR shared=1 ORDER BY shared, name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptTemplate{}
	for rows.Next() {
		var item PromptTemplate
		var ownerID string
		var shared int
		var updated int64
		if err := rows.Scan(&item.ID, &ownerID, &item.Name, &item.Body, &shared, &updated); err != nil {
			return nil, err
		}
		item.Shared = shared != 0
		item.Owner = ownerID == userID
		item.UpdatedAt = fromMS(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PromptTemplate(userID, id string) (PromptTemplate, error) {
	var item PromptTemplate
	var ownerID string
	var shared int
	var updated int64
	err := s.db.QueryRow(`SELECT id,user_id,name,body,shared,updated_at FROM prompt_templates
		WHERE id=? AND (user_id=? OR shared=1)`, id, userID).
		Scan(&item.ID, &ownerID, &item.Name, &item.Body, &shared, &updated)
	if err != nil {
		return item, err
	}
	item.Shared = shared != 0
	item.Owner = ownerID == userID
	item.UpdatedAt = fromMS(updated)
	return item, nil
}

func (s *Store) CreatePromptTemplate(userID, name, body string, shared bool) (PromptTemplate, error) {
	id := newID()
	now := ms(time.Now())
	_, err := s.db.Exec(`INSERT INTO prompt_templates(id,user_id,name,body,shared,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, userID, strings.TrimSpace(name), body, boolInt(shared), now, now)
	if err != nil {
		return PromptTemplate{}, err
	}
	return s.PromptTemplate(userID, id)
}

func (s *Store) UpdatePromptTemplate(userID, id, name, body string, shared bool) (PromptTemplate, error) {
	res, err := s.db.Exec(`UPDATE prompt_templates SET name=?,body=?,shared=?,updated_at=? WHERE id=? AND user_id=?`,
		strings.TrimSpace(name), body, boolInt(shared), ms(time.Now()), id, userID)
	if err != nil {
		return PromptTemplate{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return PromptTemplate{}, sql.ErrNoRows
	}
	return s.PromptTemplate(userID, id)
}

func (s *Store) DeletePromptTemplate(userID, id string) error {
	res, err := s.db.Exec(`DELETE FROM prompt_templates WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
