package hub

import (
	"net"
	"testing"
)

// TestNotificationSSRFRanges pins the destination filter used by notification
// channels and Web Push. Go's IsPrivate does not cover shared address space,
// which contains the Alibaba metadata service, or the other non-global blocks.
func TestNotificationSSRFRanges(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "100.100.100.200", "100.64.0.1", "100.127.255.254",
		"192.0.0.192", "198.18.0.1", "198.19.255.255", "240.0.0.1", "255.255.255.255",
		"0.0.0.0", "::1", "fc00::1", "fe80::1", "::ffff:100.100.100.200",
	}
	for _, raw := range blocked {
		if !blockedNotificationIP(net.ParseIP(raw)) {
			t.Errorf("%s must be blocked for notification delivery", raw)
		}
	}
	for _, raw := range []string{"93.184.216.34", "1.1.1.1", "2606:4700:4700::1111"} {
		if blockedNotificationIP(net.ParseIP(raw)) {
			t.Errorf("%s is a global address and must stay reachable", raw)
		}
	}
	if !blockedNotificationIP(nil) {
		t.Error("a nil address must be blocked")
	}
}
