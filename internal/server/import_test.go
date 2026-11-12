package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipOf builds a course package in memory from path → content.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func manifestJSON(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// columnManifest is a small, valid column course: two chapters, three lessons.
func columnManifest() map[string]any {
	return map[string]any{
		"version": 1,
		"title":   "Agent 系统设计",
		"type":    "column",
		"intro":   "一门讲 Agent 的专栏。",
		"author":  map[string]any{"name": "大明", "intro": "资深工程师"},
		"chapters": []any{
			map[string]any{"id": "opening", "title": "开篇", "lessons": []any{
				map[string]any{"id": "l00", "title": "开篇词", "file": "00_开篇词.md"},
			}},
			map[string]any{"id": "intent", "title": "意图识别", "lessons": []any{
				map[string]any{"id": "l01", "title": "01｜用户输入规范化", "file": "01_用户输入.md"},
				map[string]any{"id": "l02", "title": "02｜三层意图识别", "file": "02_意图识别.md"},
			}},
		},
	}
}

func columnFiles(m map[string]any) map[string]string {
	return map[string]string{
		"course.json":           manifestJSON(m),
		"00_开篇词.md":             "# 开篇词\n\n你好。\n",
		"01_用户输入.md":            "# 01\n\n![](images/1/a.png)\n\n见[官网](https://example.com)。\n",
		"02_意图识别.md":            "# 02\n\n<img src=\"images/2/b.jpg\" alt=\"\">\n",
		"images/1/a.png":        "png-bytes",
		"images/2/b.jpg":        "jpg-bytes",
		"__MACOSX/._junk":       "junk",
		"notes/unreferenced.md": "not part of the course",
	}
}

func uploadCourse(t *testing.T, c *client, files map[string]string) int {
	t.Helper()
	r := c.upload("/api/courses", zipOf(t, files))
	expectStatus(t, r, http.StatusCreated)
	return int(r.json()["id"].(float64))
}

func problemPaths(r resp) []string {
	var out []string
	if e, ok := r.json()["error"].(map[string]any); ok {
		ps, _ := e["problems"].([]any)
		for _, p := range ps {
			m := p.(map[string]any)
			path, _ := m["path"].(string)
			out = append(out, path)
		}
	}
	return out
}

func expectProblems(t *testing.T, r resp, paths ...string) {
	t.Helper()
	expectError(t, r, http.StatusUnprocessableEntity, "invalid_package")
	got := problemPaths(r)
	for _, want := range paths {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a problem at %q, got %v (%s)", want, got, r.body)
		}
	}
}

func TestUploadingAPackageCreatesAPrivateCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	c := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil)).json()
	if c["title"] != "Agent 系统设计" || c["type"] != "column" || c["visibility"] != "private" || c["publishState"] != "unpublished" {
		t.Fatalf("unexpected course: %v", c)
	}
	if c["completion"] != "finished" {
		t.Fatalf("completion should default to finished, got %v", c["completion"])
	}
	if c["intro"] != "一门讲 Agent 的专栏。" || c["authorName"] != "大明" || c["authorIntro"] != "资深工程师" {
		t.Fatalf("unexpected course info: %v", c)
	}
	chapters := c["chapters"].([]any)
	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %v", chapters)
	}
	ch := chapters[1].(map[string]any)
	lessons := ch["lessons"].([]any)
	if ch["key"] != "intent" || ch["title"] != "意图识别" || len(lessons) != 2 {
		t.Fatalf("unexpected chapter: %v", ch)
	}
	if l := lessons[0].(map[string]any); l["key"] != "l01" || l["title"] != "01｜用户输入规范化" {
		t.Fatalf("unexpected lesson: %v", l)
	}
	if c["lessonCount"] != float64(3) {
		t.Fatalf("expected 3 lessons, got %v", c["lessonCount"])
	}
}

func TestUploaderStartsLearningTheUploadedCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses")
	if len(mine) != 1 || int(mine[0].(map[string]any)["id"].(float64)) != id {
		t.Fatalf("expected the uploaded course in my courses, got %v", mine)
	}
}

func TestCompletionStatusComesFromTheManifest(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["completion"] = "incomplete"

	id := uploadCourse(t, alice, columnFiles(m))

	c := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil)).json()
	if c["completion"] != "incomplete" {
		t.Fatalf("expected incomplete, got %v", c["completion"])
	}
}

func TestPackageMayWrapEverythingInOneFolder(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	wrapped := map[string]string{}
	for k, v := range columnFiles(columnManifest()) {
		wrapped["Agent 系统设计/"+k] = v
	}

	uploadCourse(t, alice, wrapped)
}

func TestVideoCourseTakesPackagedFilesAndLinks(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := map[string]any{
		"version": 1, "title": "视频课", "type": "video", "completion": "ongoing",
		"chapters": []any{map[string]any{"id": "c1", "title": "第一章", "lessons": []any{
			map[string]any{"id": "v1", "title": "本地视频", "video": map[string]any{"file": "videos/1.mp4"}, "duration": "12:30"},
			map[string]any{"id": "v2", "title": "外链视频", "video": map[string]any{"url": "https://www.bilibili.com/video/BV1xx"}, "file": "v2.md"},
		}}},
	}

	id := uploadCourse(t, alice, map[string]string{"course.json": manifestJSON(m), "videos/1.mp4": "mp4", "v2.md": "# 文字稿"})

	c := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil)).json()
	lessons := c["chapters"].([]any)[0].(map[string]any)["lessons"].([]any)
	v1, v2 := lessons[0].(map[string]any), lessons[1].(map[string]any)
	if v1["video"] != "file" || v1["duration"] != "12:30" || v2["video"] != "url" || v2["hasText"] != true || v1["hasText"] != false {
		t.Fatalf("unexpected video lessons: %v %v", v1, v2)
	}
}

func TestPackageWithoutManifestIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	files := columnFiles(columnManifest())
	delete(files, "course.json")

	expectProblems(t, alice.upload("/api/courses", zipOf(t, files)), "course.json")
}

func TestNonZipUploadIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	r := alice.upload("/api/courses", []byte("definitely not a zip"))
	expectError(t, r, http.StatusUnprocessableEntity, "invalid_package")
}

func TestManifestProblemsAreAllReported(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := map[string]any{
		"version":    1,
		"title":      "",
		"type":       "paper",
		"completion": "almost",
		"chapters": []any{
			map[string]any{"id": "c1", "title": "一", "lessons": []any{
				map[string]any{"id": "dup", "title": "甲", "file": "a.md"},
				map[string]any{"id": "dup", "title": "", "file": "b.md"},
			}},
			map[string]any{"id": "c1", "title": "二", "lessons": []any{}},
		},
	}

	r := alice.upload("/api/courses", zipOf(t, map[string]string{"course.json": manifestJSON(m), "a.md": "a", "b.md": "b"}))

	expectProblems(t, r, "title", "type", "completion", "chapters[0].lessons[1].id", "chapters[0].lessons[1].title", "chapters[1].id", "chapters[1].lessons")
}

func TestManifestThatIsNotJSONIsRejected(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")

	expectProblems(t, alice.upload("/api/courses", zipOf(t, map[string]string{"course.json": "{nope"})), "course.json")
}

func TestUnknownManifestFieldsAreReported(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["tittle"] = "typo"

	expectProblems(t, alice.upload("/api/courses", zipOf(t, columnFiles(m))), "tittle")
}

func TestMissingReferencedFilesAreListed(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["cover"] = "cover.png"
	files := columnFiles(m)
	delete(files, "02_意图识别.md")
	delete(files, "images/1/a.png")

	r := alice.upload("/api/courses", zipOf(t, files))

	expectProblems(t, r, "cover", "chapters[1].lessons[1].file", "01_用户输入.md")
	if !strings.Contains(string(r.body), "images/1/a.png") {
		t.Fatalf("the missing image should be named: %s", r.body)
	}
}

func TestPathsMayNotEscapeThePackage(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["chapters"].([]any)[0].(map[string]any)["lessons"].([]any)[0].(map[string]any)["file"] = "../etc/passwd"

	expectProblems(t, alice.upload("/api/courses", zipOf(t, columnFiles(m))), "chapters[0].lessons[0].file")
}

func TestColumnLessonsNeedTextAndVideoLessonsNeedVideo(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	col := columnManifest()
	delete(col["chapters"].([]any)[0].(map[string]any)["lessons"].([]any)[0].(map[string]any), "file")
	vid := map[string]any{
		"version": 1, "title": "视频课", "type": "video",
		"chapters": []any{map[string]any{"id": "c1", "title": "一", "lessons": []any{
			map[string]any{"id": "v1", "title": "无视频"},
			map[string]any{"id": "v2", "title": "两个来源", "video": map[string]any{"file": "a.mp4", "url": "https://x.test/a.mp4"}},
			map[string]any{"id": "v3", "title": "坏链接", "video": map[string]any{"url": "javascript:alert(1)"}},
		}}},
	}

	expectProblems(t, alice.upload("/api/courses", zipOf(t, columnFiles(col))), "chapters[0].lessons[0].file")
	expectProblems(t, alice.upload("/api/courses", zipOf(t, map[string]string{"course.json": manifestJSON(vid), "a.mp4": "x"})),
		"chapters[0].lessons[0].video", "chapters[0].lessons[1].video", "chapters[0].lessons[2].video.url")
}

func TestFailedImportLeavesNothingBehind(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	files := columnFiles(columnManifest())
	delete(files, "images/2/b.jpg")

	expectError(t, alice.upload("/api/courses", zipOf(t, files)), http.StatusUnprocessableEntity, "invalid_package")

	if mine := alice.mustOK(alice.do("GET", "/api/my/courses", nil)).list("courses"); len(mine) != 0 {
		t.Fatalf("no course should exist, got %v", mine)
	}
	root := a.filesRoot()
	entries, _ := os.ReadDir(filepath.Join(root, "courses"))
	if len(entries) != 0 {
		t.Fatalf("no course files should be stored, found %d entries", len(entries))
	}
}

func TestOthersCannotSeeAPrivateCourse(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	if mine := bob.mustOK(bob.do("GET", "/api/my/courses", nil)).list("courses"); len(mine) != 0 {
		t.Fatalf("bob should not see alice's course, got %v", mine)
	}
}

func TestUploadingRequiresSignIn(t *testing.T) {
	a := newApp(t)

	expectError(t, a.client().upload("/api/courses", zipOf(t, columnFiles(columnManifest()))), http.StatusUnauthorized, "unauthenticated")
}
