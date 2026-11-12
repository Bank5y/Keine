package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func submitUpdate(t *testing.T, c *client, id int, files map[string]string) resp {
	t.Helper()
	return c.uploadTo("POST", fmt.Sprintf("/api/courses/%d/update-requests", id), zipOf(t, files))
}

func myUpdateRequests(t *testing.T, c *client, id int) []any {
	t.Helper()
	return c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d/update-requests", id), nil)).list("requests")
}

// publishedWith publishes a course whose manifest is changed by edit.
func publishedWith(t *testing.T, a *app, alice *client, edit func(map[string]any)) int {
	t.Helper()
	m := columnManifest()
	edit(m)
	id := uploadCourse(t, alice, columnFiles(m))
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	return id
}

func updateReviews(t *testing.T, admin *client, courseID int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews") {
		m := x.(map[string]any)
		if m["kind"] == "update" && int(m["course"].(map[string]any)["id"].(float64)) == courseID {
			out = append(out, m)
		}
	}
	return out
}

func TestUploaderUpdatesAPublishedCourseThroughReview(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true}))
	_, files := nextVersion()

	expectStatus(t, submitUpdate(t, alice, id, files), http.StatusCreated)

	// nothing changes while the request waits
	if d := courseJSON(t, bob, id); d["title"] != "Agent 系统设计" || d["lessonCount"] != float64(3) {
		t.Fatalf("the library keeps the old content during review: %v", d["title"])
	}
	reviews := updateReviews(t, admin, id)
	if len(reviews) != 1 {
		t.Fatalf("expected one update review, got %v", reviews)
	}
	ch := reviews[0]["changes"].(map[string]any)
	if fmt.Sprint(ch["added"]) != "[03｜Plan]" || fmt.Sprint(ch["removed"]) != "[开篇词]" || ch["kept"] != float64(2) {
		t.Fatalf("unexpected changes: %v", ch)
	}

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(reviews[0]["id"].(float64))), nil))

	d := courseJSON(t, bob, id)
	if d["title"] != "Agent 系统设计 (第二版)" || d["lessonCount"] != float64(3) {
		t.Fatalf("approval replaces the content: %v", d)
	}
	if s := lessonStates(t, bob, id)["l01"]; s != "done" {
		t.Fatalf("other learners keep their records, got %s", s)
	}
	if c, _ := d["contributors"].([]any); len(c) != 0 {
		t.Fatalf("the uploader is not a contributor: %v", c)
	}
	if reqs := myUpdateRequests(t, alice, id); reqs[0].(map[string]any)["state"] != "approved" {
		t.Fatalf("the requester sees the outcome: %v", reqs)
	}
}

func TestAnyoneMayCompleteAnIncompleteOrOngoingCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	incomplete := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	ongoing := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	finished := publishedCourse(t, a, alice)
	_, files := nextVersion()

	expectStatus(t, submitUpdate(t, bob, incomplete, files), http.StatusCreated)
	expectStatus(t, submitUpdate(t, bob, ongoing, files), http.StatusCreated)
	expectError(t, submitUpdate(t, bob, finished, files), http.StatusForbidden, "update_not_allowed")
	expectStatus(t, submitUpdate(t, alice, finished, files), http.StatusCreated)

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(updateReviews(t, admin, incomplete)[0]["id"].(float64))), nil))

	d := courseJSON(t, alice, incomplete)
	if fmt.Sprint(d["contributors"]) != "[bob]" || d["isUploader"] != true {
		t.Fatalf("bob contributes, alice stays the uploader: %v / %v", d["contributors"], d["isUploader"])
	}
}

func TestUpdateRequestsAreCheckedWhenSubmitted(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	_, files := nextVersion()
	delete(files, "03_plan.md")

	expectProblems(t, submitUpdate(t, alice, id, files), "chapters[1].lessons[0].file")

	if len(updateReviews(t, admin, id)) != 0 {
		t.Fatal("an invalid package never reaches the queue")
	}
}

func TestRejectedUpdateShowsItsReasonAndMayBeResubmitted(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	_, files := nextVersion()
	alice.mustOK(submitUpdate(t, alice, id, files))
	rid := int(updateReviews(t, admin, id)[0]["id"].(float64))

	expectError(t, admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/reject", rid), map[string]any{"reason": ""}), http.StatusBadRequest, "reason_required")
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/reject", rid), map[string]any{"reason": "第 3 讲缺图"}))

	r := myUpdateRequests(t, alice, id)[0].(map[string]any)
	if r["state"] != "rejected" || r["reason"] != "第 3 讲缺图" {
		t.Fatalf("the requester sees the rejection: %v", r)
	}
	if d := courseJSON(t, alice, id); d["title"] != "Agent 系统设计" {
		t.Fatal("a rejected update changes nothing")
	}
	expectStatus(t, submitUpdate(t, alice, id, files), http.StatusCreated)
}

func TestRequesterWithdrawsAnUpdate(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))
	rid := int(myUpdateRequests(t, bob, id)[0].(map[string]any)["id"].(float64))

	expectError(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d/update-requests/%d", id, rid), nil), http.StatusNotFound, "review_not_found")
	bob.mustOK(bob.do("DELETE", fmt.Sprintf("/api/courses/%d/update-requests/%d", id, rid), nil))

	if len(updateReviews(t, admin, id)) != 0 {
		t.Fatal("a withdrawn update leaves the queue")
	}
	if r := myUpdateRequests(t, bob, id)[0].(map[string]any); r["state"] != "withdrawn" {
		t.Fatalf("expected withdrawn, got %v", r["state"])
	}
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if strings.HasSuffix(k, "03_plan.md") {
			t.Fatalf("a withdrawn request's files are removed, found %s", k)
		}
	}
}

func TestApprovingOneUpdateFlagsTheOthersAsStale(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))
	carol.mustOK(submitUpdate(t, carol, id, files))
	reviews := updateReviews(t, admin, id)
	if reviews[0]["stale"] != false || reviews[1]["stale"] != false {
		t.Fatalf("fresh requests are not stale: %v", reviews)
	}

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(reviews[0]["id"].(float64))), nil))

	left := updateReviews(t, admin, id)
	if len(left) != 1 || left[0]["stale"] != true {
		t.Fatalf("the remaining request is based on old content: %v", left)
	}
}

func TestPrivateCoursesAreUpdatedDirectlyNotByRequest(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	_, files := nextVersion()

	expectError(t, submitUpdate(t, alice, id, files), http.StatusConflict, "invalid_state")
	expectError(t, submitUpdate(t, bob, id, files), http.StatusNotFound, "course_not_found")
}

func TestAdminUpdatesAPublishedCourseDirectly(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	_, files := nextVersion()

	admin.mustOK(updateCourse(t, admin, id, files))

	if d := courseJSON(t, alice, id); d["title"] != "Agent 系统设计 (第二版)" {
		t.Fatalf("admin updates apply at once: %v", d["title"])
	}
}

func TestUnpublishingSettlesPendingUpdateRequests(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "incomplete" })
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/unpublish", id), nil))

	if len(updateReviews(t, admin, id)) != 0 {
		t.Fatal("updates to a course that left the library leave the queue")
	}
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if strings.HasSuffix(k, "03_plan.md") {
			t.Fatalf("their files are removed, found %s", k)
		}
	}
}

func TestAdminDoesNotUpdateOthersPrivateCourses(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	private := uploadCourse(t, alice, columnFiles(columnManifest()))
	pending := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(requestPublish(alice, pending))
	own := uploadCourse(t, admin, columnFiles(columnManifest()))
	_, files := nextVersion()

	expectError(t, updateCourse(t, admin, private, files), http.StatusNotFound, "course_not_found")
	expectError(t, updateCourse(t, admin, pending, files), http.StatusForbidden, "forbidden")
	admin.mustOK(updateCourse(t, admin, own, files))

	if d := courseJSON(t, alice, private); d["title"] != "Agent 系统设计" {
		t.Fatalf("alice's private course must not change: %v", d["title"])
	}
}
