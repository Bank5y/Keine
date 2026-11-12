package server_test

import (
	"net/http"
	"testing"
)

func TestAdminFromEnvironmentCanLogIn(t *testing.T) {
	a := newApp(t)
	c := a.admin()

	me := c.mustOK(c.do("GET", "/api/me", nil)).json()
	if me["username"] != adminName || me["role"] != "admin" {
		t.Fatalf("unexpected me: %v", me)
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	a := newApp(t)
	c := a.client()

	expectError(t, c.login(adminName, "nope"), http.StatusUnauthorized, "invalid_credentials")
	expectError(t, c.login("nobody", "nope"), http.StatusUnauthorized, "invalid_credentials")
}

func TestRestartDoesNotOverwriteAdminPassword(t *testing.T) {
	a := newApp(t)
	b := a.restart(withAdmin(adminName, "a-different-password"))

	expectStatus(t, b.client().login(adminName, adminPass), http.StatusOK)
	expectError(t, b.client().login(adminName, "a-different-password"), http.StatusUnauthorized, "invalid_credentials")
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	a := newApp(t)

	expectError(t, a.client().do("GET", "/api/me", nil), http.StatusUnauthorized, "unauthenticated")
}

func TestLogoutEndsTheSession(t *testing.T) {
	a := newApp(t)
	c := a.admin()

	c.mustOK(c.do("DELETE", "/api/session", nil))

	expectError(t, c.do("GET", "/api/me", nil), http.StatusUnauthorized, "unauthenticated")
}

func TestUnknownLoginProviderIsRejected(t *testing.T) {
	a := newApp(t)
	c := a.client()

	r := c.do("POST", "/api/session", map[string]any{"provider": "oidc"})
	expectError(t, r, http.StatusBadRequest, "unknown_provider")
}
