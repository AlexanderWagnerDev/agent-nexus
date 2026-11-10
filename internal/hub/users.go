package hub

import (
	"net/http"
	"strings"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	users, err := s.store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isAdminRole(u.Role) {
		// Non-admins only need the directory for the host-share dialog.
		for i := range users {
			users[i].Role = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role != "owner" {
		writeError(w, http.StatusForbidden, "owner role required")
		return
	}
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, http.StatusBadRequest, "username must be 3-64 characters")
		return
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		writeError(w, http.StatusBadRequest, "role must be admin, member or viewer")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateUserWithRole(req.Username, hash, NewTOTPSecret(), req.Role)
	if err != nil {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	s.audit(u.Username, "user.created", map[string]string{"user_id": created.ID, "role": created.Role})
	created.PasswordHash, created.TOTPSecret = "", ""
	writeJSON(w, http.StatusCreated, created)
}

type updateUserRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role != "owner" {
		writeError(w, http.StatusForbidden, "owner role required")
		return
	}
	id := r.PathValue("id")
	if id == u.ID {
		writeError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		writeError(w, http.StatusBadRequest, "role must be admin, member or viewer")
		return
	}
	if err := s.store.SetUserRole(id, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "user.role_changed", map[string]string{"user_id": id, "role": req.Role})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleHostGrants(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	if !s.hostPermission(u, hostID, "manage") {
		writeError(w, http.StatusForbidden, "host manage permission required")
		return
	}
	grants, err := s.store.ListHostGrants(hostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

type grantRequest struct {
	Permissions []string `json:"permissions"`
}

func (s *Server) handleSetHostGrant(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	if !s.hostPermission(u, hostID, "manage") {
		writeError(w, http.StatusForbidden, "host manage permission required")
		return
	}
	userID := r.PathValue("userID")
	var req grantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	permissions := normalizePermissions(req.Permissions)
	if err := s.store.SetHostGrant(hostID, userID, permissions); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "host.grant_set", map[string]string{"host_id": hostID, "user_id": userID, "permissions": permissions})
	writeJSON(w, http.StatusOK, map[string]string{"status": "granted", "permissions": permissions})
}

func (s *Server) handleDeleteHostGrant(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	if !s.hostPermission(u, hostID, "manage") {
		writeError(w, http.StatusForbidden, "host manage permission required")
		return
	}
	userID := r.PathValue("userID")
	if err := s.store.DeleteHostGrant(hostID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "host.grant_deleted", map[string]string{"host_id": hostID, "user_id": userID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
