package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Bank5y/keine/internal/notify"
)

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	inbox := r.URL.Query().Get("inbox") == "1"
	p, err := s.notifications.List(r.Context(), currentUser(r).ID, page, size, inbox)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) unreadNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.notifications.Unread(r.Context(), currentUser(r).ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": n})
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	s.changeNotification(w, r, s.notifications.MarkRead)
}

func (s *Server) dismissNotification(w http.ResponseWriter, r *http.Request) {
	s.changeNotification(w, r, s.notifications.Dismiss)
}

func (s *Server) changeNotification(w http.ResponseWriter, r *http.Request, change func(ctx context.Context, userID, id int64) error) {
	id, ok := idParam(w, r, "notification_not_found", "通知不存在")
	if !ok {
		return
	}
	err := change(r.Context(), currentUser(r).ID, id)
	if errors.Is(err, notify.ErrNotFound) {
		writeError(w, http.StatusNotFound, "notification_not_found", "通知不存在")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.notifications.MarkAllRead(r.Context(), currentUser(r).ID); err != nil {
		writeInternal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
