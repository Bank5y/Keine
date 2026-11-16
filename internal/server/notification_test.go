package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func notifications(t *testing.T, c *client, query string) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", "/api/notifications"+query, nil)).json()
}

func unreadCount(t *testing.T, c *client) int {
	t.Helper()
	return int(c.mustOK(c.do("GET", "/api/notifications/unread-count", nil)).json()["unread"].(float64))
}

func notificationList(t *testing.T, c *client) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range notifications(t, c, "")["notifications"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func rejectPublish(t *testing.T, a *app, alice *client, reason string) int {
	t.Helper()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/reject", reviewID(t, admin, id)), map[string]any{"reason": reason}))
	return id
}

func TestUploaderIsNotifiedWhenAPublishRequestIsApproved(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	if unreadCount(t, alice) != 0 {
		t.Fatal("a new user has no notifications")
	}

	id := publishedCourse(t, a, alice)

	if unreadCount(t, alice) != 1 {
		t.Fatalf("expected one unread notification, got %d", unreadCount(t, alice))
	}
	list := notificationList(t, alice)
	if len(list) != 1 {
		t.Fatalf("expected one notification, got %v", list)
	}
	n := list[0]
	p := n["payload"].(map[string]any)
	if n["kind"] != "publish_approved" || n["read"] != false || int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计" {
		t.Fatalf("unexpected notification: %v", n)
	}
	if n["createdAt"] == nil {
		t.Fatalf("a notification has a time: %v", n)
	}
}

func TestUploaderIsNotifiedWithTheReasonWhenAPublishRequestIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	id := rejectPublish(t, a, alice, "第 3 章分组错了")

	list := notificationList(t, alice)
	if len(list) != 1 {
		t.Fatalf("expected one notification, got %v", list)
	}
	p := list[0]["payload"].(map[string]any)
	if list[0]["kind"] != "publish_rejected" || int(p["courseId"].(float64)) != id || p["reason"] != "第 3 章分组错了" || p["courseTitle"] != "Agent 系统设计" {
		t.Fatalf("unexpected notification: %v", list[0])
	}
}

func TestUploaderIsNotifiedWhenAdminPublishesAPendingCourseDirectly(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	list := notificationList(t, alice)
	if len(list) != 1 || list[0]["kind"] != "publish_approved" {
		t.Fatalf("publishing directly grants the request: %v", list)
	}
}

func TestUsersSeeOnlyTheirOwnNotifications(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	publishedCourse(t, a, alice)

	if unreadCount(t, bob) != 0 || len(notificationList(t, bob)) != 0 {
		t.Fatal("bob sees none of alice's notifications")
	}
	nid := int(notificationList(t, alice)[0]["id"].(float64))
	expectError(t, bob.do("POST", fmt.Sprintf("/api/notifications/%d/read", nid), nil), http.StatusNotFound, "notification_not_found")
	if unreadCount(t, alice) != 1 {
		t.Fatal("bob cannot mark alice's notification read")
	}
	expectStatus(t, a.client().do("GET", "/api/notifications", nil), http.StatusUnauthorized)
}

func TestMarkingNotificationsRead(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	publishedCourse(t, a, alice)
	rejectPublish(t, a, alice, "缺封面")
	rejectPublish(t, a, alice, "缺第二章")
	if unreadCount(t, alice) != 3 {
		t.Fatalf("expected 3 unread, got %d", unreadCount(t, alice))
	}

	list := notificationList(t, alice)
	if list[0]["payload"].(map[string]any)["reason"] != "缺第二章" {
		t.Fatalf("newest first: %v", list)
	}
	alice.mustOK(alice.do("POST", fmt.Sprintf("/api/notifications/%d/read", int(list[1]["id"].(float64))), nil))
	if unreadCount(t, alice) != 2 || notificationList(t, alice)[1]["read"] != true {
		t.Fatal("one notification read leaves two unread")
	}
	alice.mustOK(alice.do("POST", fmt.Sprintf("/api/notifications/%d/read", int(list[1]["id"].(float64))), nil)) // again is fine

	alice.mustOK(alice.do("POST", "/api/notifications/read-all", nil))
	if unreadCount(t, alice) != 0 {
		t.Fatal("all read")
	}
	for _, n := range notificationList(t, alice) {
		if n["read"] != true {
			t.Fatalf("all read: %v", n)
		}
	}
}

func TestNotificationsArePaged(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	for i := 0; i < 3; i++ {
		rejectPublish(t, a, alice, fmt.Sprintf("理由 %d", i))
	}

	first := notifications(t, alice, "?pageSize=2")
	if len(first["notifications"].([]any)) != 2 || first["total"] != float64(3) || first["unread"] != float64(3) {
		t.Fatalf("unexpected first page: %v", first)
	}
	second := notifications(t, alice, "?pageSize=2&page=2")
	rest := second["notifications"].([]any)
	if len(rest) != 1 || rest[0].(map[string]any)["payload"].(map[string]any)["reason"] != "理由 0" {
		t.Fatalf("unexpected second page: %v", second)
	}
}
