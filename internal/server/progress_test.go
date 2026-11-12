package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// lessonStates returns lesson key → state from the course detail.
func lessonStates(t *testing.T, c *client, courseID int) map[string]string {
	t.Helper()
	out := map[string]string{}
	d := c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d", courseID), nil)).json()
	for _, ch := range d["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			m := l.(map[string]any)
			out[m["key"].(string)] = m["state"].(string)
		}
	}
	return out
}

func courseProgress(t *testing.T, c *client, courseID int) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d", courseID), nil)).json()["progress"].(map[string]any)
}

func visit(t *testing.T, c *client, courseID, lessonID int) {
	t.Helper()
	c.mustOK(c.do("POST", fmt.Sprintf("/api/courses/%d/lessons/%d/visit", courseID, lessonID), nil))
}

func setProgress(c *client, courseID, lessonID int, body map[string]any) resp {
	return c.do("PUT", fmt.Sprintf("/api/courses/%d/lessons/%d/progress", courseID, lessonID), body)
}

func TestLessonsStartUnstudied(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	for key, state := range lessonStates(t, alice, id) {
		if state != "unstarted" {
			t.Fatalf("%s: expected unstarted, got %s", key, state)
		}
	}
	p := courseProgress(t, alice, id)
	if p["done"] != float64(0) || p["total"] != float64(3) || p["lastLessonId"] != nil {
		t.Fatalf("unexpected progress: %v", p)
	}
}

func TestOpeningALessonMarksItStudying(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	visit(t, alice, id, ids["l01"])

	if s := lessonStates(t, alice, id)["l01"]; s != "studying" {
		t.Fatalf("expected studying, got %s", s)
	}
	if p := courseProgress(t, alice, id); p["lastLessonId"] != float64(ids["l01"]) || p["done"] != float64(0) {
		t.Fatalf("unexpected progress: %v", p)
	}
}

func TestFinishedLessonsCountTowardsChapterAndCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	alice.mustOK(setProgress(alice, id, ids["l01"], map[string]any{"done": true}))
	alice.mustOK(setProgress(alice, id, ids["l00"], map[string]any{"done": true}))

	d := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil)).json()
	if p := d["progress"].(map[string]any); p["done"] != float64(2) || p["total"] != float64(3) {
		t.Fatalf("unexpected course progress: %v", p)
	}
	chapters := d["chapters"].([]any)
	if c := chapters[0].(map[string]any); c["done"] != float64(1) {
		t.Fatalf("first chapter: expected 1 done, got %v", c["done"])
	}
	if c := chapters[1].(map[string]any); c["done"] != float64(1) {
		t.Fatalf("second chapter: expected 1 of 2 done, got %v", c["done"])
	}
	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if mine[0].(map[string]any)["doneCount"] != float64(2) {
		t.Fatalf("my courses should show progress, got %v", mine[0])
	}
}

func TestUndoingDoneGoesBackToStudying(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)
	alice.mustOK(setProgress(alice, id, ids["l01"], map[string]any{"done": true}))

	alice.mustOK(setProgress(alice, id, ids["l01"], map[string]any{"done": false}))

	if s := lessonStates(t, alice, id)["l01"]; s != "studying" {
		t.Fatalf("expected studying after undo, got %s", s)
	}
	if p := courseProgress(t, alice, id); p["done"] != float64(0) {
		t.Fatalf("undone lesson should not count: %v", p)
	}
}

func TestVisitingAgainKeepsALessonDone(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)
	alice.mustOK(setProgress(alice, id, ids["l01"], map[string]any{"done": true}))

	visit(t, alice, id, ids["l01"])

	if s := lessonStates(t, alice, id)["l01"]; s != "done" {
		t.Fatalf("re-opening must not undo a finished lesson, got %s", s)
	}
}

func TestPositionInALessonIsRemembered(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)
	visit(t, alice, id, ids["l02"])

	alice.mustOK(setProgress(alice, id, ids["l02"], map[string]any{"position": 0.42}))

	l := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l02"]), nil)).json()
	if l["position"] != 0.42 || l["state"] != "studying" {
		t.Fatalf("expected position 0.42 while studying, got %v", l)
	}
	if p := courseProgress(t, alice, id); p["lastLessonId"] != float64(ids["l02"]) {
		t.Fatalf("expected l02 as the last lesson, got %v", p)
	}
}

func TestNegativePositionIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	expectError(t, setProgress(alice, id, ids["l01"], map[string]any{"position": -1}), http.StatusBadRequest, "bad_request")
}

func TestMyCoursesPutTheLastStudiedFirst(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	first := uploadCourse(t, alice, columnFiles(columnManifest()))
	second := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, first)

	visit(t, alice, first, ids["l00"])

	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if int(mine[0].(map[string]any)["id"].(float64)) != first || int(mine[1].(map[string]any)["id"].(float64)) != second {
		t.Fatalf("the course studied last should come first, got %v", mine)
	}
}

func TestProgressOnOthersPrivateCoursesIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	expectError(t, bob.do("POST", fmt.Sprintf("/api/courses/%d/lessons/%d/visit", id, ids["l01"]), nil), http.StatusNotFound, "course_not_found")
	expectError(t, setProgress(bob, id, ids["l01"], map[string]any{"done": true}), http.StatusNotFound, "course_not_found")
}
