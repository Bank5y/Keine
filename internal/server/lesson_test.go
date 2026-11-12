package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// lessonIDs maps lesson keys to ids for a course the client can see.
func lessonIDs(t *testing.T, c *client, courseID int) map[string]int {
	t.Helper()
	out := map[string]int{}
	d := c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d", courseID), nil)).json()
	for _, ch := range d["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			m := l.(map[string]any)
			out[m["key"].(string)] = int(m["id"].(float64))
		}
	}
	return out
}

func TestLessonTextIsServedWithItsBase(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["chapters"].([]any)[1].(map[string]any)["lessons"].([]any)[1].(map[string]any)["file"] = "notes/02_意图识别.md"
	files := columnFiles(m)
	files["notes/02_意图识别.md"] = "# 02\n\n![](../images/2/b.jpg)\n"
	id := uploadCourse(t, alice, files)
	ids := lessonIDs(t, alice, id)

	l := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l01"]), nil)).json()
	if l["title"] != "01｜用户输入规范化" || l["markdown"] != files["01_用户输入.md"] {
		t.Fatalf("unexpected lesson: %v", l)
	}
	if l["base"] != fmt.Sprintf("/api/courses/%d/files/", id) {
		t.Fatalf("unexpected base %v", l["base"])
	}
	nested := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l02"]), nil)).json()
	if nested["base"] != fmt.Sprintf("/api/courses/%d/files/notes/", id) {
		t.Fatalf("base should be the lesson file's folder, got %v", nested["base"])
	}
}

func TestLessonImagesAreServedAsInertFiles(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	r := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil))

	if string(r.body) != "png-bytes" || r.header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected image: %q %s", r.body, r.header.Get("Content-Type"))
	}
	if !strings.Contains(r.header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("course files must be served under a sandbox CSP")
	}
}

func TestOthersCannotReadPrivateLessonsOrFiles(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l01"]), nil), http.StatusNotFound, "course_not_found")
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil), http.StatusNotFound, "course_not_found")
}

func TestLessonsAndFilesBelongToTheirCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	first := uploadCourse(t, alice, columnFiles(columnManifest()))
	second := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, first)

	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", second, ids["l01"]), nil), http.StatusNotFound, "lesson_not_found")
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/nope.png", first), nil), http.StatusNotFound, "file_not_found")
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/notes/unreferenced.md", first), nil), http.StatusNotFound, "file_not_found")
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/..%%2F%d%%2Fimages%%2F1%%2Fa.png", first, second), nil), http.StatusNotFound, "file_not_found")
}
