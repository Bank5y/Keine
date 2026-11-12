package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/Bank5y/keine/internal/auth"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Problems lists individual issues, e.g. every invalid field of a course
	// manifest.
	Problems any `json:"problems,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": apiError{Code: code, Message: message}})
}

func writeProblems(w http.ResponseWriter, status int, code, message string, problems any) {
	writeJSON(w, status, map[string]any{"error": apiError{Code: code, Message: message, Problems: problems}})
}

// writeInternal logs the cause and answers with a generic 500.
func writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
}

const maxJSONBody = 1 << 20

// readJSON decodes a JSON request body into v.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "请求格式不正确")
		return false
	}
	return true
}

type ctxKey struct{}

func currentUser(r *http.Request) auth.User {
	u, _ := r.Context().Value(ctxKey{}).(auth.User)
	return u
}

// signedIn lets only requests with a live session through.
func (s *Server) signedIn(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.auth.Authenticate(r.Context(), sessionToken(r))
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "请先登录")
			return
		}
		if err != nil {
			writeInternal(w, r, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// adminOnly lets only signed-in admins through.
func (s *Server) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return s.signedIn(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		h(w, r)
	})
}

// spa serves the built web app, falling back to index.html for client routes.
func spa(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if f, err := web.Open(p); err == nil {
				st, _ := f.Stat()
				f.Close()
				if st != nil && !st.IsDir() {
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, web, "index.html")
	})
}

// idParam reads the {id} path segment. A malformed id names nothing, so it
// is answered as the not-found error of that resource.
func idParam(w http.ResponseWriter, r *http.Request, notFoundCode, notFoundMessage string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, notFoundCode, notFoundMessage)
		return 0, false
	}
	return id, true
}

func courseIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return idParam(w, r, "course_not_found", "课程不存在")
}

func userIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return idParam(w, r, "user_not_found", "用户不存在")
}
