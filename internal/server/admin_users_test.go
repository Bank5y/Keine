package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// userID looks a user up in the admin user list.
func userID(t *testing.T, admin *client, name string) int {
	t.Helper()
	for _, u := range admin.mustOK(admin.do("GET", "/api/admin/users", nil)).list("users") {
		m := u.(map[string]any)
		if m["username"] == name {
			return int(m["id"].(float64))
		}
	}
	t.Fatalf("user %q not listed", name)
	return 0
}

func TestAdminListsUsers(t *testing.T) {
	a := newApp(t)
	a.user("alice")
	admin := a.admin()

	users := admin.mustOK(admin.do("GET", "/api/admin/users", nil)).list("users")
	names := map[string]map[string]any{}
	for _, u := range users {
		m := u.(map[string]any)
		names[m["username"].(string)] = m
	}
	if len(names) != 2 || names["alice"] == nil || names[adminName] == nil {
		t.Fatalf("unexpected users: %v", users)
	}
	alice := names["alice"]
	if alice["role"] != "user" || alice["disabled"] != false || alice["createdAt"] == "" {
		t.Fatalf("unexpected alice: %v", alice)
	}
}

func TestDisabledUserIsSignedOutAndCannotSignIn(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := userID(t, admin, "alice")

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%d", id), map[string]any{"disabled": true}))

	expectError(t, alice.do("GET", "/api/me", nil), http.StatusUnauthorized, "unauthenticated")
	expectError(t, a.client().login("alice", "alice-password"), http.StatusForbidden, "account_disabled")

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%d", id), map[string]any{"disabled": false}))
	expectStatus(t, a.client().login("alice", "alice-password"), http.StatusOK)
}

func TestAdminCannotDisableThemselves(t *testing.T) {
	a := newApp(t)
	admin := a.admin()
	id := userID(t, admin, adminName)

	r := admin.do("PUT", fmt.Sprintf("/api/admin/users/%d", id), map[string]any{"disabled": true})
	expectError(t, r, http.StatusBadRequest, "cannot_disable_self")
}

func TestAdminResetsPassword(t *testing.T) {
	a := newApp(t)
	a.user("alice")
	admin := a.admin()
	id := userID(t, admin, "alice")

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%d/password", id), map[string]any{"password": "reset-by-admin"}))

	expectError(t, a.client().login("alice", "alice-password"), http.StatusUnauthorized, "invalid_credentials")
	expectStatus(t, a.client().login("alice", "reset-by-admin"), http.StatusOK)
}

func TestAdminCreatesAccountsWhileRegistrationIsClosed(t *testing.T) {
	a := newApp(t)
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/site", map[string]any{"registrationOpen": false}))

	r := admin.do("POST", "/api/users", map[string]any{"username": "bob", "password": "bob-password"})
	expectStatus(t, r, http.StatusCreated)
	expectStatus(t, a.client().login("bob", "bob-password"), http.StatusOK)
}

func TestUsersCannotUseAdminAPI(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	a.user("bob")
	id := userID(t, a.admin(), "bob")

	expectError(t, alice.do("GET", "/api/admin/users", nil), http.StatusForbidden, "forbidden")
	expectError(t, alice.do("PUT", fmt.Sprintf("/api/admin/users/%d", id), map[string]any{"disabled": true}), http.StatusForbidden, "forbidden")
	expectError(t, alice.do("PUT", fmt.Sprintf("/api/admin/users/%d/password", id), map[string]any{"password": "hijacked-password"}), http.StatusForbidden, "forbidden")
}

func TestUnknownUserIsNotFound(t *testing.T) {
	a := newApp(t)
	admin := a.admin()

	expectError(t, admin.do("PUT", "/api/admin/users/9999", map[string]any{"disabled": true}), http.StatusNotFound, "user_not_found")
	expectError(t, admin.do("PUT", "/api/admin/users/9999/password", map[string]any{"password": "whatever-password"}), http.StatusNotFound, "user_not_found")
}
