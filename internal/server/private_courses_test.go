package server_test

import (
	"fmt"
	"net/http"
	"strings"
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
	dir := fmt.Sprintf("courses/%d/", id)
	if len(a.storedKeys(dir)) == 0 {
		t.Fatal("course files should exist before deleting")
	}

	expectStatus(t, alice.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNoContent)

	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if len(mine) != 1 || int(mine[0].(map[string]any)["id"].(float64)) != keep {
		t.Fatalf("only the other course should remain, got %v", mine)
	}
	if keys := a.storedKeys(dir); len(keys) != 0 {
		t.Fatalf("course files should be removed, found %v", keys)
	}
	if len(a.storedKeys(fmt.Sprintf("courses/%d/", keep))) == 0 {
		t.Fatal("the other course's files should remain")
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

// Course files come from users; served from our origin they must not run
// scripts (an SVG cover can carry JavaScript).
func TestCoverCannotRunScripts(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["cover"] = "cover.svg"
	files := columnFiles(m)
	files["cover.svg"] = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	id := uploadCourse(t, alice, files)

	r := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/cover", id), nil))

	csp := r.header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "script-src 'none'") {
		t.Fatalf("cover should be served with a locked-down CSP, got %q", csp)
	}
}
