package server

import (
	"net/http"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/credits"
)

// listedUser is an account on the admin's list, with its plan and what
// it used of it today.
type listedUser struct {
	auth.User
	Credits credits.Account `json:"credits"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.Users(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	accounts, err := s.credits.Accounts(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	out := make([]listedUser, len(users))
	for i, u := range users {
		out[i] = listedUser{u, accounts[u.ID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
		// PlanID moves the user to another plan, at once (#130).
		PlanID *int64 `json:"planId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Disabled == nil && in.PlanID == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "没有要修改的内容")
		return
	}
	if in.Disabled != nil && *in.Disabled && id == currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "cannot_disable_self", "不能禁用自己的账号")
		return
	}
	ctx := r.Context()
	if in.PlanID != nil {
		if !writeCreditsError(w, r, s.credits.Assign(ctx, id, *in.PlanID)) {
			return
		}
	}
	u, err := s.auth.User(ctx, id)
	if in.Disabled != nil {
		u, err = s.auth.SetDisabled(ctx, id, *in.Disabled)
	}
	if !writeAccountError(w, r, err) {
		return
	}
	account, err := s.credits.Account(ctx, id)
	if !writeCreditsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, listedUser{u, account})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
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
