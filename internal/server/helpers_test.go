package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/Bank5y/keine/internal/server"
	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/testutil"
)

const (
	adminName = "root"
	adminPass = "root-password"
)

// app is one running Keine instance backed by a fresh database.
type app struct {
	t      *testing.T
	dbURL  string
	files  storage.Store
	srv    *httptest.Server
	server *server.Server
}

type appOption func(*server.Options)

func withAdmin(name, pass string) appOption {
	return func(o *server.Options) { o.AdminUsername, o.AdminPassword = name, pass }
}

func newApp(t *testing.T, opts ...appOption) *app {
	t.Helper()
	return startApp(t, testutil.NewDatabase(t), storage.NewLocal(t.TempDir()), opts...)
}

// restart starts a second instance against the same database and files,
// as a redeploy would.
func (a *app) restart(opts ...appOption) *app {
	a.t.Helper()
	a.srv.Close()
	return startApp(a.t, a.dbURL, a.files, opts...)
}

func startApp(t *testing.T, dbURL string, files storage.Store, opts ...appOption) *app {
	t.Helper()
	o := server.Options{DatabaseURL: dbURL, Storage: files, AdminUsername: adminName, AdminPassword: adminPass}
	for _, f := range opts {
		f(&o)
	}
	s, err := server.New(context.Background(), o)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); s.Close() })
	return &app{t: t, dbURL: dbURL, files: files, srv: ts, server: s}
}

// client is one browser: it keeps its own session cookie.
type client struct {
	a    *app
	http *http.Client
}

func (a *app) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{a: a, http: &http.Client{Jar: jar}}
}

// as returns a client already logged in with the given credentials.
func (a *app) as(name, pass string) *client {
	a.t.Helper()
	c := a.client()
	c.mustOK(c.login(name, pass))
	return c
}

func (a *app) admin() *client { return a.as(adminName, adminPass) }

// user registers a new user and returns a logged-in client for it.
func (a *app) user(name string) *client {
	a.t.Helper()
	c := a.client()
	c.mustOK(c.do("POST", "/api/users", map[string]any{"username": name, "password": name + "-password"}))
	c.mustOK(c.login(name, name+"-password"))
	return c
}

type resp struct {
	status int
	body   []byte
	header http.Header
}

func (r resp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.body, &m)
	return m
}

func (r resp) list(key string) []any {
	v, _ := r.json()[key].([]any)
	return v
}

func (r resp) code() string {
	if e, ok := r.json()["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (c *client) send(req *http.Request) resp {
	c.a.t.Helper()
	res, err := c.http.Do(req)
	if err != nil {
		c.a.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: b, header: res.Header}
}

func (c *client) do(method, path string, body any) resp {
	c.a.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.a.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req)
}

func (c *client) upload(path string, zipBytes []byte) resp {
	c.a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("package", "course.zip")
	_, _ = fw.Write(zipBytes)
	_ = w.Close()
	req, _ := http.NewRequest("POST", c.a.srv.URL+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.send(req)
}

func (c *client) login(name, pass string) resp {
	return c.do("POST", "/api/session", map[string]any{"provider": "password", "username": name, "password": pass})
}

func (c *client) mustOK(r resp) resp {
	c.a.t.Helper()
	if r.status < 200 || r.status > 299 {
		c.a.t.Fatalf("expected success, got %d: %s", r.status, r.body)
	}
	return r
}

func expectStatus(t *testing.T, r resp, want int) {
	t.Helper()
	if r.status != want {
		t.Fatalf("expected status %d, got %d: %s", want, r.status, r.body)
	}
}

func expectError(t *testing.T, r resp, status int, code string) {
	t.Helper()
	expectStatus(t, r, status)
	if r.code() != code {
		t.Fatalf("expected error code %q, got %q: %s", code, r.code(), r.body)
	}
}
