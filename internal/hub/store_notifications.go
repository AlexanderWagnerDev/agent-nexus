package hub

import (
	"database/sql"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

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

func (s *Store) NotificationExists(userID, runID, kind string) (bool, error) {
	if runID == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND run_id=? AND kind=?`, userID, runID, kind).Scan(&count)
	return count > 0, err
}

func (s *Store) AddNotification(userID, kind, title, body, runID, link string) error {
	_, err := s.db.Exec(`INSERT INTO notifications(id,user_id,kind,title,body,run_id,link,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		newID(), userID, kind, title, body, runID, link, ms(time.Now()))
	return err
}

func (s *Store) ListNotifications(userID string, limit int) ([]protocol.Notification, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT n.id,n.kind,n.title,n.body,n.run_id,n.link,n.created_at,n.read_at,
		COALESCE(r.host_id,''),COALESCE(h.name,''),COALESCE(s.agent,'')
		FROM notifications n
		LEFT JOIN runs r ON r.id=n.run_id AND n.run_id<>''
		LEFT JOIN hosts h ON h.id=r.host_id
		LEFT JOIN sessions s ON s.id=r.session_id
		WHERE n.user_id=? ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []protocol.Notification{}
	for rows.Next() {
		var n protocol.Notification
		var created int64
		var read sql.NullInt64
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.RunID, &n.Link, &created, &read,
			&n.HostID, &n.HostName, &n.Agent); err != nil {
			return nil, 0, err
		}
		n.CreatedAt = fromMS(created)
		n.Read = read.Valid
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&unread)
	return out, unread, nil
}

// MarkNotificationsRead marks one notification (id != "") or every unread
// notification for the user. It returns the remaining unread count.
func (s *Store) MarkNotificationsRead(userID, id string) (int, error) {
	if id != "" {
		if _, err := s.db.Exec(`UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`, ms(time.Now()), id, userID); err != nil {
			return 0, err
		}
	} else {
		if _, err := s.db.Exec(`UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, ms(time.Now()), userID); err != nil {
			return 0, err
		}
	}
	var unread int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&unread)
	return unread, err
}
