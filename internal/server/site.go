package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// siteSettings are the deployment-wide switches an admin controls.
type siteSettings struct {
	RegistrationOpen bool `json:"registrationOpen"`
}

var defaultSite = siteSettings{RegistrationOpen: true}

const siteKey = "site"

func (s *Server) loadSite(ctx context.Context) (siteSettings, error) {
	st := defaultSite
	var raw []byte
	err := s.db.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", siteKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(raw, &st)
}

// getSite is public: the sign-in page needs to know whether to offer sign-up.
func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadSite(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) putSite(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadSite(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if !readJSON(w, r, &st) {
		return
	}
	raw, _ := json.Marshal(st)
	if _, err := s.db.Exec(r.Context(), `
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, siteKey, raw); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
