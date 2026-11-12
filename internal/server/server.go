// Package server is Keine's HTTP API and the host of the embedded web app.
package server

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/db"
	"github.com/Bank5y/keine/internal/storage"
)

type Options struct {
	DatabaseURL   string
	Storage       storage.Store
	AdminUsername string
	AdminPassword string
	// SecureCookies marks the session cookie Secure; set it behind HTTPS.
	SecureCookies bool
	// Web is the built front end; nil serves the API only.
	Web fs.FS
}

type Server struct {
	opts  Options
	db    *pgxpool.Pool
	auth  *auth.Service
	files storage.Store
	mux   *http.ServeMux
}

// New connects to the database, migrates it and makes sure the deployment's
// admin account exists.
func New(ctx context.Context, opts Options) (*Server, error) {
	pool, err := db.Open(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, db: pool, auth: auth.NewService(pool), files: opts.Storage, mux: http.NewServeMux()}
	if opts.AdminUsername != "" {
		if err := s.auth.EnsureAdmin(ctx, opts.AdminUsername, opts.AdminPassword); err != nil {
			pool.Close()
			return nil, err
		}
	}
	s.routes()
	return s, nil
}

func (s *Server) Close() { s.db.Close() }

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/session", s.login)
	s.mux.HandleFunc("DELETE /api/session", s.logout)
	s.mux.HandleFunc("GET /api/me", s.signedIn(s.me))
	s.mux.HandleFunc("PUT /api/me/password", s.signedIn(s.changePassword))
	s.mux.HandleFunc("POST /api/users", s.createUser)
	s.mux.HandleFunc("GET /api/site", s.getSite)
	s.mux.HandleFunc("PUT /api/site", s.adminOnly(s.putSite))
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})
	if s.opts.Web != nil {
		s.mux.Handle("/", spa(s.opts.Web))
	}
}
