package server_test

import (
	"net/http"
	"testing"
)

func TestVisitorCanRegisterAndSignIn(t *testing.T) {
	a := newApp(t)
	c := a.client()

	r := c.do("POST", "/api/users", map[string]any{"username": "alice", "password": "alice-password"})
	expectStatus(t, r, http.StatusCreated)

	c.mustOK(c.login("alice", "alice-password"))
	me := c.mustOK(c.do("GET", "/api/me", nil)).json()
	if me["username"] != "alice" || me["role"] != "user" {
		t.Fatalf("unexpected me: %v", me)
	}
}

func TestUsernamesAreUniqueIgnoringCase(t *testing.T) {
	a := newApp(t)
	a.user("alice")

	r := a.client().do("POST", "/api/users", map[string]any{"username": "Alice", "password": "another-password"})
	expectError(t, r, http.StatusConflict, "username_taken")
}

func TestShortPasswordIsRejected(t *testing.T) {
	a := newApp(t)

	r := a.client().do("POST", "/api/users", map[string]any{"username": "alice", "password": "short"})
	expectError(t, r, http.StatusBadRequest, "weak_password")
}

func TestRegistrationIsOpenByDefault(t *testing.T) {
	a := newApp(t)

	s := a.client().mustOK(a.client().do("GET", "/api/site", nil)).json()
	if s["registrationOpen"] != true {
		t.Fatalf("expected registration open by default, got %v", s)
	}
}

func TestAdminCanCloseRegistration(t *testing.T) {
	a := newApp(t)
	admin := a.admin()

	admin.mustOK(admin.do("PUT", "/api/site", map[string]any{"registrationOpen": false}))

	visitor := a.client()
	s := visitor.mustOK(visitor.do("GET", "/api/site", nil)).json()
	if s["registrationOpen"] != false {
		t.Fatalf("expected registration closed, got %v", s)
	}
	r := visitor.do("POST", "/api/users", map[string]any{"username": "alice", "password": "alice-password"})
	expectError(t, r, http.StatusForbidden, "registration_closed")

	admin.mustOK(admin.do("PUT", "/api/site", map[string]any{"registrationOpen": true}))
	expectStatus(t, visitor.do("POST", "/api/users", map[string]any{"username": "alice", "password": "alice-password"}), http.StatusCreated)
}

func TestOnlyAdminsChangeSiteSettings(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	expectError(t, alice.do("PUT", "/api/site", map[string]any{"registrationOpen": false}), http.StatusForbidden, "forbidden")
	expectError(t, a.client().do("PUT", "/api/site", map[string]any{"registrationOpen": false}), http.StatusUnauthorized, "unauthenticated")
}

func TestUserCanChangeOwnPassword(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	alice.mustOK(alice.do("PUT", "/api/me/password", map[string]any{"current": "alice-password", "new": "a-new-password"}))

	expectError(t, a.client().login("alice", "alice-password"), http.StatusUnauthorized, "invalid_credentials")
	expectStatus(t, a.client().login("alice", "a-new-password"), http.StatusOK)
}

func TestChangingPasswordRequiresTheCurrentOne(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	r := alice.do("PUT", "/api/me/password", map[string]any{"current": "wrong-password", "new": "a-new-password"})
	expectError(t, r, http.StatusBadRequest, "wrong_password")
	expectStatus(t, a.client().login("alice", "alice-password"), http.StatusOK)
}
