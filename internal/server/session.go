package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/credits"
)

const sessionCookie = "keine_session"

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   s.opts.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var payload json.RawMessage
	if !readJSON(w, r, &payload) {
		return
	}
	var head struct {
		Provider string `json:"provider"`
	}
	_ = json.Unmarshal(payload, &head)
	token, u, err := s.auth.Login(r.Context(), head.Provider, payload)
	switch {
	case errors.Is(err, auth.ErrUnknownProvider):
		writeError(w, http.StatusBadRequest, "unknown_provider", "不支持的登录方式")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	case errors.Is(err, auth.ErrDisabled):
		writeError(w, http.StatusForbidden, "account_disabled", "该账号已被禁用, 请联系管理员")
		return
	case err != nil:
		writeInternal(w, r, err)
		return
	}
	s.setSessionCookie(w, token, 30*24*time.Hour)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if t := sessionToken(r); t != "" {
		if err := s.auth.Logout(r.Context(), t); err != nil {
			writeInternal(w, r, err)
			return
		}
	}
	s.setSessionCookie(w, "", -time.Second)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	problem, err := s.models.Problem(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	notice, err := s.models.Notice(r.Context(), u.ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	account, err := s.credits.Account(r.Context(), u.ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		auth.User
		// AIAvailable: every turn kind has a model whose search route
		// resolves, so readers get analysis and exploration.
		AIAvailable bool `json:"aiAvailable"`
		// AIProblem says why not, to the admin only, who can fix it; a
		// reader is only told it is not set up.
		AIProblem *assistant.Problem `json:"aiProblem"`
		assistant.Notice
		// Credits are the reader's credits today (ADR 0010).
		Credits credits.Account `json:"credits"`
	}{u, problem == nil, adminOnly(u, problem), notice, account})
}

// adminOnly is p for the admin, nil for anyone else.
func adminOnly(u auth.User, p *assistant.Problem) *assistant.Problem {
	if u.IsAdmin() {
		return p
	}
	return nil
}

// readNotice notes that the reader read the notice of what goes to
// third-party services (#132), which then shows on no device again.
func (s *Server) readNotice(w http.ResponseWriter, r *http.Request) {
	if err := s.models.ReadNotice(r.Context(), currentUser(r).ID); err != nil {
		writeInternal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
