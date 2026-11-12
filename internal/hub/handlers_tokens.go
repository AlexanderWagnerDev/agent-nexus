package hub

import (
	"database/sql"
	"net/http"
	"strings"
)

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	tokens, err := s.store.ListAPITokens(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 80 {
		writeError(w, http.StatusBadRequest, "name must be 1-80 characters")
		return
	}
	token, raw, err := s.store.CreateAPIToken(u.ID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "api_token.created", map[string]string{"token_id": token.ID, "scopes": "read"})
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "secret": raw})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if err := s.store.RevokeAPIToken(u.ID, r.PathValue("id")); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "api_token.revoked", map[string]string{"token_id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
