package server_test

import (
	"fmt"
	"testing"
)

// inbox is what the bell drops down: unread notifications, plus the ones
// that stay until dismissed.
func inbox(t *testing.T, c *client) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range notifications(t, c, "?inbox=1")["notifications"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func kinds(ns []map[string]any) []any {
	var out []any
	for _, n := range ns {
		out = append(out, n["kind"])
	}
	return out
}

func TestTheInboxHoldsOnlyUnreadNotifications(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	publishedCourse(t, a, alice)

	in := inbox(t, alice)
	if len(in) != 1 || in[0]["kind"] != "publish_approved" {
		t.Fatalf("an unread notification is in the inbox: %v", in)
	}
	alice.mustOK(alice.do("POST", fmt.Sprintf("/api/notifications/%d/read", int(in[0]["id"].(float64))), nil))

	if in := inbox(t, alice); len(in) != 0 {
		t.Fatalf("a read notification leaves the inbox: %v", in)
	}
	if all := notificationList(t, alice); len(all) != 1 {
		t.Fatalf("but stays in the full list: %v", all)
	}
}

// pendingFor leaves one of erin's annotations awaiting migration and
// returns the annotation and the notification about it.
func pendingFor(t *testing.T, a *app) (erin *client, annotation, notice map[string]any) {
	t.Helper()
	alice := a.user("alice")
	erin = a.user("erin")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	subscribe(t, erin, id)
	annotation = annotate(t, erin, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(rewordedOpening())))
	ns := notificationsOfKind(t, erin, "annotations_pending")
	if len(ns) != 1 {
		t.Fatalf("expected one pending notice, got %v", ns)
	}
	return erin, annotation, ns[0]
}

func TestAPendingAnnotationsNoticeStaysInTheInboxUntilDismissed(t *testing.T) {
	a := newApp(t)
	erin, _, notice := pendingFor(t, a)
	nid := int(notice["id"].(float64))
	erin.mustOK(erin.do("POST", "/api/notifications/read-all", nil))

	in := inbox(t, erin)
	if len(in) != 1 || int(in[0]["id"].(float64)) != nid || in[0]["read"] != true || in[0]["sticky"] != true {
		t.Fatalf("read, the pending notice still stays in the inbox: %v", in)
	}
	if unreadCount(t, erin) != 0 {
		t.Fatal("but no longer counts as unread")
	}

	erin.mustOK(erin.do("POST", fmt.Sprintf("/api/notifications/%d/dismiss", nid), nil))

	if in := inbox(t, erin); len(in) != 0 {
		t.Fatalf("dismissed, it leaves the inbox: %v", in)
	}
	if len(notificationsOfKind(t, erin, "annotations_pending")) != 1 {
		t.Fatal("but stays in the full list")
	}
}

func TestDismissingSomeoneElsesNotificationIsNotFound(t *testing.T) {
	a := newApp(t)
	_, _, notice := pendingFor(t, a)
	mallory := a.user("mallory")
	expectError(t, mallory.do("POST", fmt.Sprintf("/api/notifications/%d/dismiss", int(notice["id"].(float64))), nil), 404, "notification_not_found")
}

func TestAPendingAnnotationsNoticeLeavesTheInboxOnceEveryAnnotationIsHandled(t *testing.T) {
	a := newApp(t)
	erin, ann, _ := pendingFor(t, a)
	erin.mustOK(erin.do("POST", "/api/notifications/read-all", nil))

	erin.mustOK(erin.do("POST", fmt.Sprintf("/api/annotations/%d/excerpt", int(ann["id"].(float64))), nil))

	if in := inbox(t, erin); len(in) != 0 {
		t.Fatalf("nothing awaits migration any more, so the notice goes: %v", in)
	}
}

func TestDeletingTheLastPendingAnnotationAlsoClearsTheNotice(t *testing.T) {
	a := newApp(t)
	erin, ann, _ := pendingFor(t, a)

	erin.mustOK(erin.do("DELETE", fmt.Sprintf("/api/annotations/%d", int(ann["id"].(float64))), nil))

	if in := inbox(t, erin); len(in) != 1 || in[0]["kind"] != "course_updated" {
		t.Fatalf("nothing awaits migration any more, so only the unread course_updated stays: %v", kinds(in))
	}
	if unreadCount(t, erin) != 1 {
		t.Fatal("the course_updated notice is unaffected")
	}
}

func TestAdminsAreToldOfAPublishRequestUntilItIsDecided(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))

	ns := notificationsOfKind(t, admin, "publish_requested")
	if len(ns) != 1 || ns[0]["read"] != false {
		t.Fatalf("expected one unread publish_requested, got %v", ns)
	}
	p := ns[0]["payload"].(map[string]any)
	if int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计" || p["requester"] != "alice" || p["requestId"] == nil {
		t.Fatalf("unexpected payload: %v", p)
	}

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", reviewID(t, admin, id)), nil))

	if in := inbox(t, admin); len(in) != 0 {
		t.Fatalf("a decided request leaves every admin's inbox: %v", kinds(in))
	}
}

func TestAdminsAreToldAgainWhenARejectedCourseIsResubmitted(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := rejectPublish(t, a, alice, "第 3 章分组错了")
	alice.mustOK(requestPublish(alice, id))

	ns := notificationsOfKind(t, admin, "publish_requested")
	if len(ns) != 2 || ns[0]["read"] != false || ns[1]["read"] != true {
		t.Fatalf("a resubmission is a new request: %v", ns)
	}
}

func TestAdminsAreToldOfAnUpdateRequestUntilItIsWithdrawn(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	admin.mustOK(admin.do("POST", "/api/notifications/read-all", nil))
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))

	ns := notificationsOfKind(t, admin, "update_requested")
	if len(ns) != 1 || ns[0]["read"] != false || ns[0]["payload"].(map[string]any)["requester"] != "bob" {
		t.Fatalf("expected one unread update_requested from bob, got %v", ns)
	}
	if n := notificationsOfKind(t, bob, "update_requested"); len(n) != 0 {
		t.Fatalf("only admins are told: %v", n)
	}

	rid := int(myUpdateRequests(t, bob, id)[0].(map[string]any)["id"].(float64))
	bob.mustOK(bob.do("DELETE", fmt.Sprintf("/api/courses/%d/update-requests/%d", id, rid), nil))

	if in := inbox(t, admin); len(in) != 0 {
		t.Fatalf("a withdrawn request leaves the inbox: %v", kinds(in))
	}
}

func TestALaterUpdateThatReattachesEverythingClearsTheNotice(t *testing.T) {
	a := newApp(t)
	erin, _, _ := pendingFor(t, a)
	id := courseOfPending(t, erin)

	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(openingText)))

	for _, n := range inbox(t, erin) {
		if n["kind"] == "annotations_pending" {
			t.Fatalf("the quote is back, nothing awaits migration, so the notice goes: %v", kinds(inbox(t, erin)))
		}
	}
}

func courseOfPending(t *testing.T, c *client) int {
	t.Helper()
	return int(notificationsOfKind(t, c, "annotations_pending")[0]["payload"].(map[string]any)["courseId"].(float64))
}
