package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type NotificationChannel struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Target    string            `json:"target,omitempty"`
	Config    map[string]string `json:"-"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (s *Store) ListNotificationChannels(userID string) ([]NotificationChannel, error) {
	rows, err := s.db.Query(`SELECT id,kind,name,enabled,updated_at FROM notification_channels WHERE user_id=? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		var channel NotificationChannel
		var enabled int
		var updated int64
		if err := rows.Scan(&channel.ID, &channel.Kind, &channel.Name, &enabled, &updated); err != nil {
			return nil, err
		}
		channel.Enabled = enabled != 0
		channel.UpdatedAt = fromMS(updated)
		channel.Target = channelTarget(channel.Kind, channel.Name)
		out = append(out, channel)
	}
	return out, rows.Err()
}

// channelTarget deliberately returns only a non-secret label. URLs and tokens
// are never returned to the browser after creation.
func channelTarget(kind, name string) string {
	if name != "" {
		return name
	}
	return kind
}

func (s *Store) CreateNotificationChannel(userID, kind, name string, config map[string]string) (NotificationChannel, error) {
	if s.secrets == nil {
		return NotificationChannel{}, errors.New("secret encryption is not configured")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return NotificationChannel{}, err
	}
	sealed, err := s.seal(string(raw))
	if err != nil {
		return NotificationChannel{}, err
	}
	id := newID()
	now := ms(time.Now())
	_, err = s.db.Exec(`INSERT INTO notification_channels(id,user_id,kind,name,config,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,1,?,?)`, id, userID, kind, name, sealed, now, now)
	if err != nil {
		return NotificationChannel{}, err
	}
	return NotificationChannel{ID: id, Kind: kind, Name: name, Enabled: true, Target: name, UpdatedAt: fromMS(now)}, nil
}

func (s *Store) UpdateNotificationChannel(userID, id, kind, name string, config map[string]string, enabled bool) (NotificationChannel, error) {
	if s.secrets == nil {
		return NotificationChannel{}, errors.New("secret encryption is not configured")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return NotificationChannel{}, err
	}
	sealed, err := s.seal(string(raw))
	if err != nil {
		return NotificationChannel{}, err
	}
	now := ms(time.Now())
	res, err := s.db.Exec(`UPDATE notification_channels SET kind=?,name=?,config=?,enabled=?,updated_at=? WHERE id=? AND user_id=?`,
		kind, name, sealed, boolInt(enabled), now, id, userID)
	if err != nil {
		return NotificationChannel{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotificationChannel{}, sql.ErrNoRows
	}
	return NotificationChannel{ID: id, Kind: kind, Name: name, Enabled: enabled, Target: name, UpdatedAt: fromMS(now)}, nil
}

func (s *Store) DeleteNotificationChannel(userID, id string) error {
	res, err := s.db.Exec(`DELETE FROM notification_channels WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) NotificationChannelByID(userID, id string) (NotificationChannel, error) {
	var channel NotificationChannel
	var sealed string
	var enabled int
	var updated int64
	err := s.db.QueryRow(`SELECT id,kind,name,config,enabled,updated_at FROM notification_channels WHERE id=? AND user_id=?`, id, userID).
		Scan(&channel.ID, &channel.Kind, &channel.Name, &sealed, &enabled, &updated)
	if err != nil {
		return channel, err
	}
	plain, err := s.open(sealed)
	if err != nil {
		return channel, err
	}
	if json.Unmarshal([]byte(plain), &channel.Config) != nil {
		return channel, errors.New("invalid channel configuration")
	}
	channel.Enabled = enabled != 0
	channel.UpdatedAt = fromMS(updated)
	return channel, nil
}

// EnabledNotificationChannels decrypts the channel configuration for delivery
// only. Callers must not log the returned config.
func (s *Store) EnabledNotificationChannels(userID string) ([]NotificationChannel, error) {
	rows, err := s.db.Query(`SELECT id,kind,name,config,updated_at FROM notification_channels WHERE user_id=? AND enabled=1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		var channel NotificationChannel
		var sealed string
		var updated int64
		if err := rows.Scan(&channel.ID, &channel.Kind, &channel.Name, &sealed, &updated); err != nil {
			return nil, err
		}
		plain, err := s.open(sealed)
		if err != nil {
			continue
		}
		if json.Unmarshal([]byte(plain), &channel.Config) != nil {
			continue
		}
		channel.Enabled = true
		channel.UpdatedAt = fromMS(updated)
		out = append(out, channel)
	}
	return out, rows.Err()
}
