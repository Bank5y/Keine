package server

import (
	"errors"
	"net/http"

	"github.com/Bank5y/keine/internal/auth"
)

// createUser is self-registration for visitors and account creation for
// admins; only admins may create accounts while registration is closed.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	caller, err := s.auth.Authenticate(r.Context(), sessionToken(r))
	if err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
		writeInternal(w, r, err)
		return
	}
	if !caller.IsAdmin() {
		st, err := s.loadSite(r.Context())
		if err != nil {
			writeInternal(w, r, err)
			return
		}
		if !st.RegistrationOpen {
			writeError(w, http.StatusForbidden, "registration_closed", "本站已关闭注册, 请联系管理员创建账号")
			return
		}
	}
	u, err := s.auth.CreateUser(r.Context(), in.Username, in.Password, auth.RoleUser)
	if !writeAccountError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.auth.ChangePassword(r.Context(), currentUser(r).ID, in.Current, in.New)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusBadRequest, "wrong_password", "当前密码不正确")
		return
	}
	if !writeAccountError(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeAccountError answers the account errors shared by several endpoints.
// It reports whether err was nil.
func writeAccountError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "用户名已被占用")
	case errors.Is(err, auth.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, "invalid_username", "用户名不能为空, 最长 32 个字符, 且不能包含空白开头结尾、/ 或 @")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "weak_password", "密码至少 8 个字符")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
	default:
		writeInternal(w, r, err)
	}
	return false
}
