package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var notificationKinds = map[string]bool{
	"ntfy": true, "gotify": true, "telegram": true, "discord": true, "slack": true, "webhook": true,
}

func notificationEndpoint(kind string, config map[string]string) (string, string, error) {
	switch kind {
	case "ntfy", "discord", "slack", "webhook":
		if config["url"] == "" {
			return "", "", errors.New("url is required")
		}
		return config["url"], "", nil
	case "gotify":
		if config["url"] == "" || config["token"] == "" {
			return "", "", errors.New("url and token are required")
		}
		return strings.TrimRight(config["url"], "/") + "/message?token=" + url.QueryEscape(config["token"]), "", nil
	case "telegram":
		if config["token"] == "" || config["chat_id"] == "" {
			return "", "", errors.New("token and chat_id are required")
		}
		return "https://api.telegram.org/bot" + config["token"] + "/sendMessage", config["chat_id"], nil
	default:
		return "", "", errors.New("unknown notification channel kind")
	}
}

func blockedNotificationIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func validateNotificationURL(raw string, allowPrivate bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("URL must be an absolute http(s) URL")
	}
	if parsed.User != nil {
		return errors.New("URL must not contain credentials")
	}
	if allowPrivate {
		return nil
	}
	host := parsed.Hostname()
	if strings.HasSuffix(strings.ToLower(host), ".local") || strings.HasSuffix(strings.ToLower(host), ".internal") {
		return errors.New("internal host names are not allowed")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve notification host: %w", err)
	}
	for _, ip := range ips {
		if blockedNotificationIP(ip) {
			return errors.New("notification target resolves to a private or local address")
		}
	}
	return nil
}

func validateNotificationConfig(kind, name string, config map[string]string, allowPrivate bool) error {
	if !notificationKinds[kind] {
		return errors.New("unknown notification channel kind")
	}
	if strings.TrimSpace(name) == "" || len(name) > 80 {
		return errors.New("name must be 1-80 characters")
	}
	endpoint, _, err := notificationEndpoint(kind, config)
	if err != nil {
		return err
	}
	if kind == "telegram" {
		// The token is part of the URL path; validate the fixed API host
		// without requiring a DNS lookup of api.telegram.org.
		return nil
	}
	return validateNotificationURL(endpoint, allowPrivate)
}

func (s *Server) notificationHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !s.notificationAllowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if blockedNotificationIP(net.ParseIP(host)) {
				return errors.New("notification connection to a private address blocked")
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 6 * time.Second,
		},
	}
}

type notificationPayload struct {
	Title string
	Body  string
	Link  string
}

func (s *Server) channelRequest(ctx context.Context, kind string, config map[string]string, payload notificationPayload) (*http.Request, error) {
	endpoint, chatID, err := notificationEndpoint(kind, config)
	if err != nil {
		return nil, err
	}
	var body []byte
	contentType := "application/json"
	switch kind {
	case "ntfy":
		body = []byte(payload.Title + "\n" + payload.Body)
		contentType = "text/plain; charset=utf-8"
	case "gotify":
		body, _ = json.Marshal(map[string]any{"title": payload.Title, "message": payload.Body, "priority": 4})
	case "telegram":
		body, _ = json.Marshal(map[string]string{"chat_id": chatID, "text": payload.Title + "\n" + payload.Body})
	case "discord":
		body, _ = json.Marshal(map[string]string{"content": payload.Title + "\n" + payload.Body})
	case "slack":
		body, _ = json.Marshal(map[string]string{"text": payload.Title + "\n" + payload.Body})
	default:
		body, _ = json.Marshal(map[string]string{
			"title": payload.Title, "body": payload.Body, "url": payload.Link,
		})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "AgentNexus/"+Version)
	if kind == "gotify" {
		req.Header.Set("X-Gotify-Key", config["token"])
	}
	return req, nil
}

func (s *Server) deliverNotificationChannel(channel NotificationChannel, title, body, link string) error {
	if _, _, err := notificationEndpoint(channel.Kind, channel.Config); err != nil {
		return err
	}
	payload := notificationPayload{Title: title, Body: body, Link: link}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := s.channelRequest(context.Background(), channel.Kind, channel.Config, payload)
		if err != nil {
			return err
		}
		resp, err := s.notificationHTTPClient().Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("channel returned HTTP %d", resp.StatusCode)
			if resp.StatusCode < 500 {
				return lastErr
			}
		} else {
			// Do not return the transport error: it can contain a webhook URL
			// or Telegram bot token.
			lastErr = errors.New("notification request failed")
		}
		if attempt == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	return lastErr
}

// deliverNotificationChannels sends content-free alerts to every enabled
// channel. The caller has already applied user/host preferences.
func (s *Server) deliverNotificationChannels(ownerID, kind, title, body, link string) {
	channels, err := s.store.EnabledNotificationChannels(ownerID)
	if err != nil {
		s.log.Debug("cannot load notification channels", "error", err)
		return
	}
	for _, channel := range channels {
		if err := s.deliverNotificationChannel(channel, title, body, link); err != nil {
			s.log.Warn("notification channel delivery failed", "channel_id", channel.ID, "kind", channel.Kind, "error", err)
			s.audit("notification:"+channel.Kind, "notification_channel.failed", map[string]string{"channel_id": channel.ID})
			continue
		}
		s.log.Debug("notification channel delivered", "channel_id", channel.ID, "kind", channel.Kind, "event", kind)
	}
}
