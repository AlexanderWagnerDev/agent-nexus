package hub

import (
	"database/sql"
	"net/http"
	"strings"
)

type templateRequest struct {
	Name   string `json:"name"`
	Body   string `json:"body"`
	Shared bool   `json:"shared"`
}

func validateTemplate(req templateRequest) string {
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 80 {
		return "name must be 1-80 characters"
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 32<<10 {
		return "body must be 1-32768 characters"
	}
	return ""
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	items, err := s.store.ListPromptTemplates(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": items})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req templateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := validateTemplate(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Shared && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "only admins can share templates")
		return
	}
	item, err := s.store.CreatePromptTemplate(u.ID, req.Name, req.Body, req.Shared)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "template.created", map[string]string{"template_id": item.ID, "shared": boolString(req.Shared)})
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req templateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := validateTemplate(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Shared && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "only admins can share templates")
		return
	}
	item, err := s.store.UpdatePromptTemplate(u.ID, r.PathValue("id"), req.Name, req.Body, req.Shared)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "template.updated", map[string]string{"template_id": item.ID})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if err := s.store.DeletePromptTemplate(u.ID, r.PathValue("id")); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "template.deleted", map[string]string{"template_id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
