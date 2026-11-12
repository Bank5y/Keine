package server_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func myCourseIDsWhere(t *testing.T, c *client, q url.Values) []int {
	t.Helper()
	var ids []int
	for _, x := range c.mustOK(c.do("GET", "/api/my/courses?"+q.Encode(), nil)).list("courses") {
		ids = append(ids, int(x.(map[string]any)["id"].(float64)))
	}
	return ids
}

func TestRemovingALibraryCourseKeepsItsProgress(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil))
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true}))

	expectStatus(t, bob.do("DELETE", fmt.Sprintf("/api/courses/%d/enroll", id), nil), http.StatusNoContent)

	if contains(myCourseIDs(t, bob), id) {
		t.Fatal("the course should leave my courses")
	}
	if d := courseJSON(t, bob, id); d["enrolled"] != false {
		t.Fatalf("expected not enrolled after removing, got %v", d["enrolled"])
	}

	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil))

	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("starting again brings the course back")
	}
	if s := lessonStates(t, bob, id)["l01"]; s != "done" {
		t.Fatalf("progress should be restored, got %s", s)
	}
}

func TestOpeningALessonBringsARemovedCourseBack(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))
	ids := lessonIDs(t, bob, id)
	visit(t, bob, id, ids["l01"])
	bob.mustOK(bob.do("DELETE", fmt.Sprintf("/api/courses/%d/enroll", id), nil))

	visit(t, bob, id, ids["l02"])

	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("opening a lesson starts learning again")
	}
}

func TestPrivateCoursesCannotBeRemovedOnlyDeleted(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d/enroll", id), nil), http.StatusConflict, "invalid_state")
	if !contains(myCourseIDs(t, alice), id) {
		t.Fatal("the private course stays")
	}
}

func TestMyCoursesFilterByStudyStateTypeAndCompletion(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["completion"] = "ongoing"
	studying := uploadCourse(t, alice, columnFiles(m))
	done := uploadCourse(t, alice, columnFiles(columnManifest()))
	for _, lid := range lessonIDs(t, alice, done) {
		alice.mustOK(setProgress(alice, done, lid, map[string]any{"done": true}))
	}
	vm := map[string]any{
		"version": 1, "title": "视频", "type": "video",
		"chapters": []any{map[string]any{"id": "c", "title": "一", "lessons": []any{map[string]any{"id": "v", "title": "v", "video": map[string]any{"url": "https://example.com/v.mp4"}}}}},
	}
	video := uploadCourse(t, alice, map[string]string{"course.json": manifestJSON(vm)})

	if ids := myCourseIDsWhere(t, alice, url.Values{"state": {"done"}}); len(ids) != 1 || ids[0] != done {
		t.Fatalf("done filter: %v", ids)
	}
	if ids := myCourseIDsWhere(t, alice, url.Values{"state": {"studying"}}); len(ids) != 2 || contains(ids, done) {
		t.Fatalf("studying filter: %v", ids)
	}
	if ids := myCourseIDsWhere(t, alice, url.Values{"type": {"video"}}); len(ids) != 1 || ids[0] != video {
		t.Fatalf("type filter: %v", ids)
	}
	if ids := myCourseIDsWhere(t, alice, url.Values{"completion": {"ongoing"}}); len(ids) != 1 || ids[0] != studying {
		t.Fatalf("completion filter: %v", ids)
	}
}

func TestAnUnpublishedCourseLeavesOtherLearnersCourses(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true}))
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil))

	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/unpublish", id), nil))

	if contains(myCourseIDs(t, bob), id) {
		t.Fatal("bob can no longer see the course")
	}
	if !contains(myCourseIDs(t, alice), id) {
		t.Fatal("the uploader keeps it as a private course")
	}

	alice.mustOK(requestPublish(alice, id))
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", reviewID(t, admin, id)), nil))

	if !contains(myCourseIDs(t, bob), id) || lessonStates(t, bob, id)["l01"] != "done" {
		t.Fatal("published again, the course returns with bob's records")
	}
}
