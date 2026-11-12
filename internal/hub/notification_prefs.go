package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func defaultNotificationPrefs() map[string]bool {
	return map[string]bool{
		"approval": true, "completed": true, "failed": true, "disconnected": true,
	}
}

func mergeNotificationPrefs(base, overrides map[string]bool) {
	for kind, enabled := range overrides {
		switch kind {
		case "approval", "completed", "failed", "disconnected":
			base[kind] = enabled
		}
	}
}

func normalizeNotificationPrefs(prefs map[string]bool) map[string]bool {
	out := map[string]bool{}
	mergeNotificationPrefs(out, prefs)
	return out
}

// NotificationHostPrefs returns the stored per-host override for one user.
// Missing entries are absent rather than defaulted: the caller merges them
// over the global preferences.
func (s *Store) NotificationHostPrefs(userID, hostID string) (map[string]bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT prefs FROM user_notification_hosts WHERE user_id=? AND host_id=?`, userID, hostID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	prefs := map[string]bool{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	return normalizeNotificationPrefs(prefs), nil
}

func (s *Store) SetNotificationHostPrefs(userID, hostID string, prefs map[string]bool) error {
	raw, err := json.Marshal(normalizeNotificationPrefs(prefs))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO user_notification_hosts(user_id,host_id,prefs,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id,host_id) DO UPDATE SET prefs=excluded.prefs, updated_at=excluded.updated_at`,
		userID, hostID, string(raw), ms(time.Now()))
	return err
}

func (s *Store) DeleteNotificationHostPrefs(userID, hostID string) error {
	_, err := s.db.Exec(`DELETE FROM user_notification_hosts WHERE user_id=? AND host_id=?`, userID, hostID)
	return err
}

// ListNotificationHostPrefs returns every stored override for a user.
func (s *Store) ListNotificationHostPrefs(userID string) (map[string]map[string]bool, error) {
	rows, err := s.db.Query(`SELECT host_id,prefs FROM user_notification_hosts WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var hostID, raw string
		if err := rows.Scan(&hostID, &raw); err != nil {
			return nil, err
		}
		prefs := map[string]bool{}
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &prefs)
		}
		out[hostID] = normalizeNotificationPrefs(prefs)
	}
	return out, rows.Err()
}

// EffectiveNotificationPrefs merges the global defaults, the user's global
// preferences and the per-host override in that order. An unset value stays
// enabled so notifications keep working for older accounts.
func (s *Store) EffectiveNotificationPrefs(userID, hostID string) (map[string]bool, error) {
	out := defaultNotificationPrefs()
	global, err := s.NotificationPrefs(userID)
	if err != nil {
		return nil, err
	}
	mergeNotificationPrefs(out, global)
	if hostID != "" {
		host, err := s.NotificationHostPrefs(userID, hostID)
		if err != nil {
			return nil, err
		}
		mergeNotificationPrefs(out, host)
	}
	return out, nil
}
