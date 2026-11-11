package hub

import "net/http"

// ---- projects ----

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	projects, err := s.store.ListProjects(u.ID, isAdminRole(u.Role), atoiOr(r.URL.Query().Get("limit"), 500))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// ---- favorites ----

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sessions, err := s.store.FavoriteSessions(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids, err := s.store.FavoriteSessionIDs(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "ids": ids})
}

func (s *Server) handleSetFavorite(w http.ResponseWriter, r *http.Request, favorite bool) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	if err := s.store.SetSessionFavorite(u.ID, sess.ID, favorite); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorite": favorite})
}

// ---- notification history ----

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	items, unread, err := s.store.ListNotifications(u.ID, atoiOr(r.URL.Query().Get("limit"), 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": items, "unread": unread})
}

func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	unread, err := s.store.MarkNotificationsRead(u.ID, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread})
}
