package server_test

import (
	"fmt"
	"testing"
)

func subscribe(t *testing.T, c *client, id int) {
	t.Helper()
	c.mustOK(c.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
}

// revisedVersion is nextVersion with l02's text left as it was: one lesson
// added (l03), one removed (l00), one whose text changed (l01), while l02
// only keeps its place.
func revisedVersion() map[string]string {
	_, files := nextVersion()
	old := columnFiles(columnManifest())
	files["02_意图识别.md"] = old["02_意图识别.md"]
	files["images/2/b.jpg"] = old["images/2/b.jpg"]
	return files
}

func onlyNotification(t *testing.T, c *client, kind string) map[string]any {
	t.Helper()
	list := notificationsOfKind(t, c, kind)
	if len(list) != 1 {
		t.Fatalf("expected one %s notification, got %v", kind, notificationList(t, c))
	}
	return list[0]["payload"].(map[string]any)
}

func TestSubscribersAreToldWhatAnApprovedUpdateChanged(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	carol := a.user("carol")
	dave := a.user("dave")
	erin := a.user("erin")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	subscribe(t, bob, id)   // the requester
	subscribe(t, carol, id) // a subscriber
	subscribe(t, erin, id)
	erin.mustOK(erin.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
	lessonIDs(t, dave, id) // reads, never subscribed
	bob.mustOK(submitUpdate(t, bob, id, revisedVersion()))

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(updateReviews(t, admin, id)[0]["id"].(float64))), nil))

	p := onlyNotification(t, carol, "course_updated")
	if int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计 (第二版)" ||
		p["added"] != float64(1) || p["removed"] != float64(1) || p["modified"] != float64(1) {
		t.Fatalf("unexpected notification: %v", p)
	}
	onlyNotification(t, alice, "course_updated") // uploading subscribes
	for name, c := range map[string]*client{"the requester": bob, "a reader who never subscribed": dave, "a former subscriber": erin} {
		if n := notificationsOfKind(t, c, "course_updated"); len(n) != 0 {
			t.Fatalf("%s is not told: %v", name, n)
		}
	}
}

func TestUploaderAndSubscribersAreToldOfUnpublishing(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	carol := a.user("carol")
	dave := a.user("dave")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	alice.mustOK(alice.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil)) // still the uploader
	subscribe(t, carol, id)
	lessonIDs(t, dave, id)
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))   // not subscribed
	carol.mustOK(submitUpdate(t, carol, id, files)) // subscribed

	unpublish(t, a, id)

	for name, c := range map[string]*client{"the uploader": alice, "a subscriber": carol} {
		p := onlyNotification(t, c, "course_unpublished")
		if int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计" {
			t.Fatalf("%s: unexpected notification: %v", name, p)
		}
	}
	if n := notificationList(t, dave); len(n) != 0 {
		t.Fatalf("a reader who never subscribed is not told: %v", n)
	}
	if n := notificationsOfKind(t, admin, "course_unpublished"); len(n) != 0 {
		t.Fatalf("the admin who unpublished is not told: %v", n)
	}
	if p := onlyNotification(t, bob, "update_rejected"); p["reason"] != "课程已从课程库下架" || int(p["courseId"].(float64)) != id {
		t.Fatalf("a requester learns why their update was turned down: %v", p)
	}
	if n := notificationsOfKind(t, carol, "update_rejected"); len(n) != 0 {
		t.Fatalf("a requester already told of the unpublishing is not told twice: %v", n)
	}
}

func TestSubscribersAreToldOfDeletionWithTheLessonsTheyRetain(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	carol := a.user("carol")
	dave := a.user("dave")
	erin := a.user("erin")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	subscribe(t, bob, id)
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true})) // bob retains l01
	subscribe(t, carol, id) // finished nothing
	lessonIDs(t, dave, id)
	_, files := nextVersion()
	erin.mustOK(submitUpdate(t, erin, id, files))

	admin.mustOK(deleteCourse(admin, id))

	if p := onlyNotification(t, bob, "course_deleted"); int(p["courseId"].(float64)) != id || p["courseTitle"] != "Agent 系统设计" || p["retainedCount"] != float64(1) {
		t.Fatalf("unexpected notification: %v", p)
	}
	if p := onlyNotification(t, carol, "course_deleted"); p["retainedCount"] != float64(0) {
		t.Fatalf("carol retains nothing: %v", p)
	}
	onlyNotification(t, alice, "course_deleted") // uploading subscribes
	if n := notificationList(t, dave); len(n) != 0 {
		t.Fatalf("a reader who never subscribed is not told: %v", n)
	}
	if n := notificationsOfKind(t, admin, "course_deleted"); len(n) != 0 {
		t.Fatalf("whoever deleted it is not told: %v", n)
	}
	if p := onlyNotification(t, erin, "update_rejected"); p["reason"] != "课程已删除" {
		t.Fatalf("a requester learns why their update was turned down: %v", p)
	}
}

func TestDeletingACourseNobodyRetainsStillTellsItsPeople(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(alice.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
	unpublish(t, a, id)
	alice.mustOK(requestPublish(alice, id)) // her own private course again, unsubscribed
	other := publishedCourse(t, a, a.user("bob"))
	subscribe(t, carol, other)

	admin.mustOK(deleteCourse(admin, id))
	admin.mustOK(deleteCourse(admin, other))

	if p := onlyNotification(t, alice, "publish_rejected"); p["reason"] != "课程已删除" || int(p["courseId"].(float64)) != id {
		t.Fatalf("the uploader learns why their publish request was turned down: %v", p)
	}
	if p := onlyNotification(t, carol, "course_deleted"); p["retainedCount"] != float64(0) || int(p["courseId"].(float64)) != other {
		t.Fatalf("a subscriber who retains nothing is told too: %v", p)
	}
}

func TestSubscribersAreToldOfAnAdminsDirectUpdate(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	subscribe(t, carol, id)
	subscribe(t, admin, id) // the admin makes the update

	admin.mustOK(updateCourse(t, admin, id, revisedVersion()))

	p := onlyNotification(t, carol, "course_updated")
	if p["added"] != float64(1) || p["removed"] != float64(1) || p["modified"] != float64(1) || p["courseTitle"] != "Agent 系统设计 (第二版)" {
		t.Fatalf("unexpected notification: %v", p)
	}
	if n := notificationsOfKind(t, admin, "course_updated"); len(n) != 0 {
		t.Fatalf("whoever made the update is not told: %v", n)
	}
}
