package hub

import (
	"strings"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

var validPermissions = map[string]bool{
	"view": true, "read_transcript": true, "resume": true, "create": true,
	"fork": true, "terminal_input": true, "approve": true, "manage": true,
}

func normalizePermissions(list []string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range list {
		p = strings.TrimSpace(strings.ToLower(p))
		if !validPermissions[p] || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		out = []string{"view", "read_transcript"}
	}
	return strings.Join(out, ",")
}

func grantAllows(perms, need string) bool {
	if perms == "" {
		return false
	}
	for _, p := range strings.Split(perms, ",") {
		if p == need || p == "manage" {
			return true
		}
	}
	return false
}

// hostPermission applies hub RBAC and per-host grants. The host policy is an
// additional, independent check performed by nexusd.
func (s *Server) hostPermission(u User, hostID, need string) bool {
	if isAdminRole(u.Role) {
		return true
	}
	host, err := s.store.HostByID(hostID)
	if err == nil && host.OwnerID == u.ID {
		return true
	}
	perms, err := s.store.HostGrantPermissions(hostID, u.ID)
	return err == nil && grantAllows(perms, need)
}

func (s *Server) sessionPermission(u User, sess protocol.SessionView, need string) bool {
	return s.hostPermission(u, sess.HostID, need)
}

// isAdminRole reports whether a role may administer users and hosts.
func isAdminRole(role string) bool {
	return role == "owner" || role == "admin"
}
