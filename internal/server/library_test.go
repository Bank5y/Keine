package server_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func libraryIDs(t *testing.T, c *client, query url.Values) []int {
	t.Helper()
	path := "/api/library"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var ids []int
	for _, x := range c.mustOK(c.do("GET", path, nil)).list("courses") {
		ids = append(ids, int(x.(map[string]any)["id"].(float64)))
	}
	return ids
}

func myCourseIDs(t *testing.T, c *client) []int {
	t.Helper()
	var ids []int
	for _, x := range c.mustOK(c.do("GET", "/api/my/courses", nil)).list("courses") {
		ids = append(ids, int(x.(map[string]any)["id"].(float64)))
	}
	return ids
}

func contains(ids []int, id int) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// publish uploads a course built from m as the admin and publishes it.
func publishAs(t *testing.T, a *app, files map[string]string) int {
	t.Helper()
	admin := a.admin()
	id := uploadCourse(t, admin, files)
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	return id
}

func TestLibraryListsOnlyPublicCourses(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	private := uploadCourse(t, alice, columnFiles(columnManifest()))
	public := publishedCourse(t, a, alice)

	for _, c := range []*client{alice, bob} {
		ids := libraryIDs(t, c, nil)
		if !contains(ids, public) || contains(ids, private) {
			t.Fatalf("library should hold the public course only, got %v", ids)
		}
	}
}

func TestLibraryFiltersAndSearches(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	column := publishAs(t, a, columnFiles(columnManifest()))
	vm := map[string]any{
		"version": 1, "title": "Rust 视频课", "type": "video", "completion": "ongoing", "author": map[string]any{"name": "Ferris"},
		"chapters": []any{map[string]any{"id": "c", "title": "一", "lessons": []any{map[string]any{"id": "v", "title": "v", "video": map[string]any{"url": "https://example.com/v.mp4"}}}}},
	}
	video := publishAs(t, a, map[string]string{"course.json": manifestJSON(vm)})

	if ids := libraryIDs(t, bob, url.Values{"type": {"video"}}); len(ids) != 1 || ids[0] != video {
		t.Fatalf("type filter: %v", ids)
	}
	if ids := libraryIDs(t, bob, url.Values{"completion": {"finished"}}); len(ids) != 1 || ids[0] != column {
		t.Fatalf("completion filter: %v", ids)
	}
	if ids := libraryIDs(t, bob, url.Values{"q": {"agent"}}); len(ids) != 1 || ids[0] != column {
		t.Fatalf("title search should ignore case: %v", ids)
	}
	if ids := libraryIDs(t, bob, url.Values{"q": {"ferris"}}); len(ids) != 1 || ids[0] != video {
		t.Fatalf("author search: %v", ids)
	}
	if ids := libraryIDs(t, bob, url.Values{"q": {"100%"}}); len(ids) != 0 {
		t.Fatalf("search treats wildcards literally: %v", ids)
	}
}

func TestStartingToLearnALibraryCourse(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))
	if contains(myCourseIDs(t, bob), id) {
		t.Fatal("not started yet")
	}
	if d := courseJSON(t, bob, id); d["enrolled"] != false {
		t.Fatalf("expected not enrolled, got %v", d["enrolled"])
	}

	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil))

	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("the course should be in my courses")
	}
	if d := courseJSON(t, bob, id); d["enrolled"] != true {
		t.Fatalf("expected enrolled, got %v", d["enrolled"])
	}
	lib := bob.mustOK(bob.do("GET", "/api/library", nil)).list("courses")
	if lib[0].(map[string]any)["enrolled"] != true {
		t.Fatalf("the library card should show it is started: %v", lib[0])
	}
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil)) // starting again is harmless
}

func TestOpeningALessonStartsLearningTheCourse(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))

	visit(t, bob, id, lessonIDs(t, bob, id)["l01"])

	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("opening a lesson should start learning the course")
	}
	if p := courseProgress(t, bob, id); p["lastLessonId"] != float64(lessonIDs(t, bob, id)["l01"]) {
		t.Fatalf("expected the lesson as the last one, got %v", p)
	}
}

func TestProgressIsPerUser(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	carol := a.user("carol")
	id := publishAs(t, a, columnFiles(columnManifest()))
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true}))

	if s := lessonStates(t, carol, id)["l01"]; s != "unstarted" {
		t.Fatalf("carol has not studied anything, got %s", s)
	}
}

func TestCannotStartLearningAnInvisibleCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, bob.do("POST", fmt.Sprintf("/api/courses/%d/enroll", id), nil), http.StatusNotFound, "course_not_found")
}
