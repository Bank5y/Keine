package server_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestMyCoursesShowWhatACardNeeds(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["completion"] = "ongoing"
	m["cover"] = "cover.png"
	files := columnFiles(m)
	files["cover.png"] = "cover-bytes"
	uploadCourse(t, alice, files)

	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	c := mine[0].(map[string]any)
	want := map[string]any{
		"title": "Agent 系统设计", "type": "column", "completion": "ongoing", "authorName": "大明",
		"visibility": "private", "publishState": "unpublished", "hasCover": true, "isUploader": true, "lessonCount": float64(3),
	}
	for k, v := range want {
		if c[k] != v {
			t.Fatalf("%s: expected %v, got %v (%v)", k, v, c[k], c)
		}
	}
}

func TestCoverIsServedToThoseWhoMaySeeTheCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	m := columnManifest()
	m["cover"] = "art/cover.png"
	files := columnFiles(m)
	files["art/cover.png"] = "cover-bytes"
	id := uploadCourse(t, alice, files)
	noCover := uploadCourse(t, alice, columnFiles(columnManifest()))

	r := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/cover", id), nil))
	if string(r.body) != "cover-bytes" || r.header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected cover: %q %s", r.body, r.header.Get("Content-Type"))
	}
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d/cover", id), nil), http.StatusNotFound, "course_not_found")
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/cover", noCover), nil), http.StatusNotFound, "cover_not_found")
}

func TestUploaderDeletesAPrivateCourseWithItsFiles(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	keep := uploadCourse(t, alice, columnFiles(columnManifest()))
	dir := filepath.Join(a.filesRoot(), "courses", fmt.Sprint(id))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("course files should exist before deleting: %v", err)
	}

	expectStatus(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNoContent)

	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if len(mine) != 1 || int(mine[0].(map[string]any)["id"].(float64)) != keep {
		t.Fatalf("only the other course should remain, got %v", mine)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("course files should be removed, stat: %v", err)
	}
	alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", keep), nil))
}

func TestOthersCannotDeleteAPrivateCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, bob.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil))
}
