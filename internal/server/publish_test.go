package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func courseJSON(t *testing.T, c *client, id int) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d", id), nil)).json()
}

func requestPublish(c *client, id int) resp {
	return c.do("POST", fmt.Sprintf("/api/courses/%d/publish-request", id), nil)
}

// pendingReview finds the review request for a course in the admin queue.
func pendingReview(t *testing.T, admin *client, courseID int) map[string]any {
	t.Helper()
	for _, x := range admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews") {
		m := x.(map[string]any)
		if int(m["course"].(map[string]any)["id"].(float64)) == courseID {
			return m
		}
	}
	t.Fatalf("no pending review for course %d", courseID)
	return nil
}

func reviewID(t *testing.T, admin *client, courseID int) int {
	return int(pendingReview(t, admin, courseID)["id"].(float64))
}

func expectPublishState(t *testing.T, c *client, id int, visibility, state string) {
	t.Helper()
	d := courseJSON(t, c, id)
	if d["visibility"] != visibility || d["publishState"] != state {
		t.Fatalf("expected %s/%s, got %s/%s", visibility, state, d["visibility"], d["publishState"])
	}
}

// publishedCourse uploads a course as alice and has the admin approve it.
func publishedCourse(t *testing.T, a *app, alice *client) int {
	t.Helper()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", reviewID(t, admin, id)), nil))
	return id
}

func TestUploaderRequestsPublishingAndAdminApproves(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	alice.mustOK(requestPublish(alice, id))
	expectPublishState(t, alice, id, "private", "pending")
	review := pendingReview(t, admin, id)
	if review["kind"] != "publish" || review["requester"].(map[string]any)["username"] != "alice" {
		t.Fatalf("unexpected review: %v", review)
	}

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(review["id"].(float64))), nil))

	expectPublishState(t, alice, id, "public", "unpublished")
	courseJSON(t, bob, id) // everyone can see it now
	if len(admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews")) != 0 {
		t.Fatal("the queue should be empty after approving")
	}
}

func TestAdminReadsCoursesUnderReviewOnly(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	expectError(t, admin.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")

	alice.mustOK(requestPublish(alice, id))

	courseJSON(t, admin, id)
	admin.mustOK(admin.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l01"]), nil))
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
}

func TestUploaderWithdrawsARequest(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))

	alice.mustOK(alice.do("DELETE", fmt.Sprintf("/api/courses/%d/publish-request", id), nil))

	expectPublishState(t, alice, id, "private", "unpublished")
	if len(admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews")) != 0 {
		t.Fatal("a withdrawn request leaves the queue")
	}
}

func TestRejectionNeedsAReasonTheUploaderCanRead(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))
	rid := reviewID(t, admin, id)

	expectError(t, admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/reject", rid), map[string]any{"reason": "  "}), http.StatusBadRequest, "reason_required")
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/reject", rid), map[string]any{"reason": "第 3 章分组错了"}))

	d := courseJSON(t, alice, id)
	if d["publishState"] != "rejected" || d["rejectReason"] != "第 3 章分组错了" {
		t.Fatalf("expected a rejection with its reason, got %v / %v", d["publishState"], d["rejectReason"])
	}
	alice.mustOK(requestPublish(alice, id)) // may ask again
	expectPublishState(t, alice, id, "private", "pending")
}

func TestOnlyTheUploaderRequestsAndOnlyFromTheRightState(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, requestPublish(bob, id), http.StatusNotFound, "course_not_found")
	expectError(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d/publish-request", id), nil), http.StatusConflict, "invalid_state")
	alice.mustOK(requestPublish(alice, id))
	expectError(t, requestPublish(alice, id), http.StatusConflict, "invalid_state")

	published := publishedCourse(t, a, alice)
	expectError(t, requestPublish(alice, published), http.StatusConflict, "invalid_state")
}

func TestPublishedCourseIsReadOnlyForTheUploader(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := publishedCourse(t, a, alice)
	alice.mustOK(setProgress(alice, id, lessonIDs(t, alice, id)["l01"], map[string]any{"done": true}))

	expectError(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusForbidden, "forbidden")
	expectError(t, alice.do("PATCH", fmt.Sprintf("/api/admin/courses/%d", id), map[string]any{"title": "改名"}), http.StatusForbidden, "forbidden")

	d := courseJSON(t, alice, id)
	if d["isUploader"] != true || d["progress"].(map[string]any)["done"] != float64(1) {
		t.Fatalf("the uploader keeps the course and its progress: %v", d["progress"])
	}
	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if len(mine) != 1 {
		t.Fatalf("the published course stays in the uploader's courses, got %v", mine)
	}
}

func TestAdminPublishesDirectly(t *testing.T) {
	a := newApp(t)
	admin := a.admin()
	alice := a.user("alice")
	own := uploadCourse(t, admin, columnFiles(columnManifest()))
	expectPublishState(t, admin, own, "private", "unpublished")

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", own), nil))

	expectPublishState(t, alice, own, "public", "unpublished")
}

func TestAdminPublishesAPendingCourseDirectly(t *testing.T) {
	a := newApp(t)
	admin := a.admin()
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	expectPublishState(t, alice, id, "public", "unpublished")
	if len(admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews")) != 0 {
		t.Fatal("publishing directly settles the pending request")
	}
}

func TestAdminUnpublishesBackToTheUploader(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	courseJSON(t, bob, id)

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/unpublish", id), nil))

	expectPublishState(t, alice, id, "private", "unpublished")
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	expectError(t, admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/unpublish", id), nil), http.StatusConflict, "invalid_state")
}

func TestAdminEditsAPublishedCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)

	admin.mustOK(admin.do("PATCH", fmt.Sprintf("/api/admin/courses/%d", id), map[string]any{
		"title": "新课名", "intro": "新的介绍", "authorName": "小明", "authorIntro": "讲师", "completion": "ongoing",
	}))

	d := courseJSON(t, alice, id)
	if d["title"] != "新课名" || d["intro"] != "新的介绍" || d["authorName"] != "小明" || d["completion"] != "ongoing" {
		t.Fatalf("edit not applied: %v", d)
	}
	expectError(t, admin.do("PATCH", fmt.Sprintf("/api/admin/courses/%d", id), map[string]any{"title": ""}), http.StatusBadRequest, "bad_request")
	expectError(t, admin.do("PATCH", fmt.Sprintf("/api/admin/courses/%d", id), map[string]any{"completion": "nope"}), http.StatusBadRequest, "bad_request")
}

func TestAdminDeletesAnyCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	published := publishedCourse(t, a, alice)
	private := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectStatus(t, admin.do("DELETE", fmt.Sprintf("/api/courses/%d", published), nil), http.StatusNoContent)
	expectStatus(t, admin.do("DELETE", fmt.Sprintf("/api/courses/%d", private), nil), http.StatusNoContent)

	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d", published), nil), http.StatusNotFound, "course_not_found")
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d", private), nil), http.StatusNotFound, "course_not_found")
}

func TestUsersCannotModerate(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, id))

	expectError(t, alice.do("GET", "/api/admin/reviews", nil), http.StatusForbidden, "forbidden")
	expectError(t, alice.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil), http.StatusForbidden, "forbidden")
	expectError(t, alice.do("POST", "/api/admin/reviews/1/approve", nil), http.StatusForbidden, "forbidden")
}

func TestAdminEditsOnlyPublishedCourses(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, admin.do("PATCH", fmt.Sprintf("/api/admin/courses/%d", id), map[string]any{"title": "偷偷改名"}), http.StatusConflict, "invalid_state")

	if d := courseJSON(t, alice, id); d["title"] != "Agent 系统设计" {
		t.Fatalf("a private course is the uploader's alone: %v", d["title"])
	}
}
