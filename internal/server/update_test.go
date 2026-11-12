package server_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func (c *client) uploadTo(method, path string, zipBytes []byte) resp {
	c.a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("package", "course.zip")
	_, _ = fw.Write(zipBytes)
	_ = w.Close()
	req, _ := http.NewRequest(method, c.a.srv.URL+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.send(req)
}

func updateCourse(t *testing.T, c *client, id int, files map[string]string) resp {
	t.Helper()
	return c.uploadTo("PUT", fmt.Sprintf("/api/courses/%d/package", id), zipOf(t, files))
}

// nextVersion is the column course after a round of changes: the opening
// lesson is gone, l01 is renamed, and a new chapter brings l03.
func nextVersion() (map[string]any, map[string]string) {
	m := map[string]any{
		"version": 1, "title": "Agent 系统设计 (第二版)", "type": "column", "completion": "ongoing",
		"chapters": []any{
			map[string]any{"id": "intent", "title": "意图识别", "lessons": []any{
				map[string]any{"id": "l01", "title": "01｜用户输入规范化 (修订)", "file": "01_用户输入.md"},
				map[string]any{"id": "l02", "title": "02｜三层意图识别", "file": "02_意图识别.md"},
			}},
			map[string]any{"id": "plan", "title": "计划", "lessons": []any{
				map[string]any{"id": "l03", "title": "03｜Plan", "file": "03_plan.md"},
			}},
		},
	}
	files := map[string]string{
		"course.json":      manifestJSON(m),
		"01_用户输入.md":       "# 01 修订版\n\n![](images/1/new.png)\n",
		"02_意图识别.md":       "# 02\n",
		"03_plan.md":       "# 03\n",
		"images/1/new.png": "new-png",
	}
	return m, files
}

func TestUploaderUpdatesAPrivateCourseKeepingProgress(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	before := lessonIDs(t, alice, id)
	alice.mustOK(setProgress(alice, id, before["l00"], map[string]any{"done": true}))
	alice.mustOK(setProgress(alice, id, before["l01"], map[string]any{"done": true}))
	_, files := nextVersion()

	alice.mustOK(updateCourse(t, alice, id, files))

	d := courseJSON(t, alice, id)
	if d["title"] != "Agent 系统设计 (第二版)" || d["completion"] != "ongoing" || d["lessonCount"] != float64(3) {
		t.Fatalf("course should follow the new manifest: %v", d)
	}
	after := lessonIDs(t, alice, id)
	if after["l01"] != before["l01"] || after["l02"] != before["l02"] {
		t.Fatal("unchanged lesson keys keep their lessons (and their links)")
	}
	if _, ok := after["l00"]; ok {
		t.Fatal("the removed lesson should be gone")
	}
	states := lessonStates(t, alice, id)
	if states["l01"] != "done" || states["l02"] != "unstarted" || states["l03"] != "unstarted" {
		t.Fatalf("unexpected states after the update: %v", states)
	}
	if p := courseProgress(t, alice, id); p["done"] != float64(1) || p["total"] != float64(3) {
		t.Fatalf("a removed lesson no longer counts: %v", p)
	}
	l := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, after["l01"]), nil)).json()
	if l["title"] != "01｜用户输入规范化 (修订)" || !strings.Contains(l["markdown"].(string), "修订版") {
		t.Fatalf("the lesson should carry the new text: %v", l)
	}
}

func TestARemovedLessonComingBackRestoresItsProgress(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(setProgress(alice, id, lessonIDs(t, alice, id)["l00"], map[string]any{"done": true}))
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))

	alice.mustOK(updateCourse(t, alice, id, columnFiles(columnManifest())))

	if s := lessonStates(t, alice, id)["l00"]; s != "done" {
		t.Fatalf("l00 came back and should be done again, got %s", s)
	}
}

func TestUpdateReplacesTheStoredFiles(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	_, files := nextVersion()

	alice.mustOK(updateCourse(t, alice, id, files))

	expectStatus(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/new.png", id), nil), http.StatusOK)
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil), http.StatusNotFound, "file_not_found")
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if strings.HasSuffix(k, "/images/1/a.png") || strings.HasSuffix(k, "00_开篇词.md") {
			t.Fatalf("files of the old version should be removed, found %s", k)
		}
	}
}

func TestFailedUpdateKeepsTheOldCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)
	_, files := nextVersion()
	delete(files, "images/1/new.png")
	keysBefore := a.storedKeys(fmt.Sprintf("courses/%d/", id))

	expectProblems(t, updateCourse(t, alice, id, files), "01_用户输入.md")

	if d := courseJSON(t, alice, id); d["title"] != "Agent 系统设计" || d["lessonCount"] != float64(3) {
		t.Fatalf("the course should be unchanged: %v", d)
	}
	alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["l01"]), nil))
	expectStatus(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil), http.StatusOK)
	if after := a.storedKeys(fmt.Sprintf("courses/%d/", id)); strings.Join(after, ",") != strings.Join(keysBefore, ",") {
		t.Fatalf("no files should be added or removed: %v → %v", keysBefore, after)
	}
}

func TestOnlyTheUploaderUpdatesAPrivateCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	_, files := nextVersion()

	expectError(t, updateCourse(t, bob, id, files), http.StatusNotFound, "course_not_found")
}

func TestPublishedCoursesAreNotUpdatedDirectlyByTheUploader(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := publishedCourse(t, a, alice)
	_, files := nextVersion()

	expectError(t, updateCourse(t, alice, id, files), http.StatusConflict, "update_request_required")
}
