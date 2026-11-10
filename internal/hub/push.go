package hub

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/SherClockHolmes/webpush-go"
)

func (s *Server) ensureVAPID() {
	pub, err := s.store.GetSetting("vapid_public")
	if err != nil || pub == "" {
		priv, public, genErr := webpush.GenerateVAPIDKeys()
		if genErr != nil {
			s.log.Warn("cannot generate VAPID keys", "error", genErr)
			return
		}
		_ = s.store.SetSetting("vapid_public", public)
		_ = s.store.SetSetting("vapid_private", priv)
		pub = public
	}
	priv, _ := s.store.GetSetting("vapid_private")
	s.vapidPublic, s.vapidPrivate = pub, priv
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.vapidPublic == "" {
		writeError(w, http.StatusServiceUnavailable, "push is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": s.vapidPublic})
}

type pushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req pushSubscribeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !strings.HasPrefix(req.Endpoint, "https://") || req.Keys.P256DH == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "invalid push subscription")
		return
	}
	u := userFrom(r)
	d := deviceFrom(r)
	if err := s.store.SavePushSubscription(u.ID, d.ID, req.Endpoint, req.Keys.P256DH, req.Keys.Auth); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "push.subscribed", map[string]string{"device_id": d.ID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "subscribed"})
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req pushUnsubscribeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	u := userFrom(r)
	if err := s.store.DeletePushSubscription(u.ID, req.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// notifyRun sends a content-free Web Push message to the run owner's devices.
// It deliberately contains no transcript, tool command, path or approval text.
func (s *Server) notifyRun(runID, title, body string) {
	if s.vapidPrivate == "" || s.vapidPublic == "" {
		return
	}
	_, ownerID, err := s.store.RunByID(runID)
	if err != nil {
		return
	}
	subs, err := s.store.PushSubscriptions(ownerID)
	if err != nil || len(subs) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"url":   "/#/runs/" + runID,
	})
	for _, sub := range subs {
		response, err := webpush.SendNotification(payload, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber:      "mailto:agentnexus@localhost",
			VAPIDPublicKey:  s.vapidPublic,
			VAPIDPrivateKey: s.vapidPrivate,
			TTL:             60,
			Urgency:         webpush.UrgencyNormal,
		})
		if err != nil {
			s.log.Debug("push delivery failed", "error", err)
			continue
		}
		if response != nil {
			if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
				s.store.DeletePushSubscriptionByID(sub.ID)
			}
			_ = response.Body.Close()
		}
	}
}
