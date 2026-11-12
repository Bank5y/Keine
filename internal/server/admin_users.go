package server

import (
	"net/http"
	"strconv"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.Users(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Disabled == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "没有要修改的内容")
		return
	}
	if *in.Disabled && id == currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "cannot_disable_self", "不能禁用自己的账号")
		return
	}
	u, err := s.auth.SetDisabled(r.Context(), id, *in.Disabled)
	if !writeAccountError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.auth.User(r.Context(), id); !writeAccountError(w, r, err) {
		return
	}
	if err := s.auth.SetPassword(r.Context(), id, in.Password); !writeAccountError(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
