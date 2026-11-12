// Package server is Keine's HTTP API and the host of the embedded web app.
package server

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/course"
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
	// MaxUploadBytes caps the size of a course package (default 4 GiB).
	MaxUploadBytes int64
}

type Server struct {
	opts    Options
	db      *pgxpool.Pool
	auth    *auth.Service
	courses *course.Service
	// generator helps users write a course manifest
	generator course.Generator
	files     storage.Store
	mux       *http.ServeMux
}

// New connects to the database, migrates it and makes sure the deployment's
// admin account exists.
func New(ctx context.Context, opts Options) (*Server, error) {
	pool, err := db.Open(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, db: pool, auth: auth.NewService(pool), courses: course.NewService(pool, opts.Storage), generator: course.PromptTemplate{}, files: opts.Storage, mux: http.NewServeMux()}
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
	s.mux.HandleFunc("GET /api/admin/users", s.adminOnly(s.listUsers))
	s.mux.HandleFunc("PUT /api/admin/users/{id}", s.adminOnly(s.updateUser))
	s.mux.HandleFunc("PUT /api/admin/users/{id}/password", s.adminOnly(s.resetPassword))
	s.mux.HandleFunc("POST /api/courses", s.signedIn(s.uploadCourse))
	s.mux.HandleFunc("GET /api/courses/{id}", s.signedIn(s.getCourse))
	s.mux.HandleFunc("DELETE /api/courses/{id}", s.signedIn(s.deleteCourse))
	s.mux.HandleFunc("PUT /api/courses/{id}/package", s.signedIn(s.updateCoursePackage))
	s.mux.HandleFunc("GET /api/courses/{id}/cover", s.signedIn(s.courseCover))
	s.mux.HandleFunc("GET /api/courses/{id}/lessons/{lesson}", s.signedIn(s.getLesson))
	s.mux.HandleFunc("POST /api/courses/{id}/lessons/{lesson}/visit", s.signedIn(s.visitLesson))
	s.mux.HandleFunc("PUT /api/courses/{id}/lessons/{lesson}/progress", s.signedIn(s.setProgress))
	s.mux.HandleFunc("GET /api/courses/{id}/files/{path...}", s.signedIn(s.courseFile))
	s.mux.HandleFunc("POST /api/courses/{id}/publish-request", s.signedIn(s.courseAction(s.requestPublish)))
	s.mux.HandleFunc("DELETE /api/courses/{id}/publish-request", s.signedIn(s.courseAction(s.withdrawPublish)))
	s.mux.HandleFunc("POST /api/courses/{id}/update-requests", s.signedIn(s.submitUpdate))
	s.mux.HandleFunc("GET /api/courses/{id}/update-requests", s.signedIn(s.myUpdateRequests))
	s.mux.HandleFunc("DELETE /api/courses/{id}/update-requests/{request}", s.signedIn(s.withdrawUpdate))
	s.mux.HandleFunc("GET /api/admin/reviews", s.adminOnly(s.listReviews))
	s.mux.HandleFunc("POST /api/admin/reviews/{id}/approve", s.adminOnly(s.approveReview))
	s.mux.HandleFunc("POST /api/admin/reviews/{id}/reject", s.adminOnly(s.rejectReview))
	s.mux.HandleFunc("POST /api/admin/courses/{id}/publish", s.adminOnly(s.courseAction(s.adminPublish)))
	s.mux.HandleFunc("POST /api/admin/courses/{id}/unpublish", s.adminOnly(s.courseAction(s.adminUnpublish)))
	s.mux.HandleFunc("PATCH /api/admin/courses/{id}", s.adminOnly(s.adminEditCourse))
	s.mux.HandleFunc("GET /api/library", s.signedIn(s.library))
	s.mux.HandleFunc("POST /api/courses/{id}/enroll", s.signedIn(s.courseAction(s.enroll)))
	s.mux.HandleFunc("GET /api/my/courses", s.signedIn(s.myCourses))
	s.mux.HandleFunc("GET /api/manifest/prompt", s.signedIn(s.manifestPrompt))
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})
	if s.opts.Web != nil {
		s.mux.Handle("/", spa(s.opts.Web))
	}
}
