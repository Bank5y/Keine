package server_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// outlineManifest is an ongoing column course whose outline runs ahead of
// its content: l02 is an upcoming lesson and the "plan" chapter is empty.
func outlineManifest() map[string]any {
	return map[string]any{
		"version": 1, "title": "Agent 系统设计", "type": "column", "completion": "ongoing",
		"chapters": []any{
			map[string]any{"id": "intent", "title": "意图识别", "lessons": []any{
				map[string]any{"id": "l01", "title": "01｜用户输入规范化", "file": "01.md"},
				map[string]any{"id": "l02", "title": "02｜三层意图识别"},
				map[string]any{"id": "l03", "title": "03｜意图路由", "file": "03.md"},
			}},
			map[string]any{"id": "plan", "title": "计划", "lessons": []any{}},
		},
	}
}

func outlineFiles(m map[string]any) map[string]string {
	return map[string]string{"course.json": manifestJSON(m), "01.md": "# 01\n", "03.md": "# 03\n"}
}

// chaptersByKey maps chapter keys to the chapters of a course detail.
func chaptersByKey(d map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, ch := range d["chapters"].([]any) {
		m := ch.(map[string]any)
		out[m["key"].(string)] = m
	}
	return out
}

func TestAnOutlineWithUpcomingLessonsAndAnEmptyChapterImports(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")

	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))

	d := courseJSON(t, alice, id)
	chapters := chaptersByKey(d)
	if len(chapters) != 2 {
		t.Fatalf("both chapters show, the empty one too: %v", d["chapters"])
	}
	if ls := chapters["plan"]["lessons"].([]any); len(ls) != 0 {
		t.Fatalf("the empty chapter has no lessons: %v", ls)
	}
	lessons := lessonsByKey(d)
	if lessons["l02"]["upcoming"] != true || lessons["l01"]["upcoming"] == true || lessons["l03"]["upcoming"] == true {
		t.Fatalf("only l02 is upcoming: %v", lessons)
	}
	if d["lessonCount"] != float64(2) {
		t.Fatalf("upcoming lessons do not count: %v", d["lessonCount"])
	}
}

func TestAFinishedCourseCannotHaveUpcomingLessons(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	m := outlineManifest()
	m["completion"] = "finished"

	r := alice.upload("/api/courses", zipOf(t, outlineFiles(m)))

	expectProblems(t, r, "completion")
	if body := string(r.body); !strings.Contains(body, "待更新") {
		t.Fatalf("the problem says why: %s", body)
	}
}

func TestUpcomingLessonsAreLeftOutOfProgress(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))
	ids := lessonIDs(t, alice, id)

	alice.mustOK(setProgress(alice, id, ids["l01"], map[string]any{"done": true}))
	alice.mustOK(setProgress(alice, id, ids["l03"], map[string]any{"done": true}))

	if p := courseProgress(t, alice, id); p["done"] != float64(2) || p["total"] != float64(2) {
		t.Fatalf("the course is learned through once every lesson with content is: %v", p)
	}
	intent := chaptersByKey(courseJSON(t, alice, id))["intent"]
	if intent["done"] != float64(2) || intent["total"] != float64(2) {
		t.Fatalf("the chapter counts only lessons with content: %v", intent)
	}
	if got := myCourseIDsWhere(t, alice, url.Values{"state": {"done"}}); len(got) != 1 || got[0] != id {
		t.Fatalf("the course counts as done in my courses: %v", got)
	}
}

func TestAnUpcomingLessonCannotBeRead(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))
	upcoming := lessonIDs(t, alice, id)["l02"]

	expectError(t, lessonText(alice, id, upcoming), http.StatusConflict, "lesson_upcoming")
	expectError(t, alice.do("POST", fmt.Sprintf("/api/courses/%d/lessons/%d/visit", id, upcoming), nil), http.StatusConflict, "lesson_upcoming")
	expectError(t, setProgress(alice, id, upcoming, map[string]any{"done": true}), http.StatusConflict, "lesson_upcoming")
}

// filledOutline is the outline once l02 has its text: l01 and l03 stay as
// they were, and the plan chapter gains an upcoming lesson l04.
func filledOutline() map[string]string {
	m := outlineManifest()
	chapters := m["chapters"].([]any)
	chapters[0].(map[string]any)["lessons"].([]any)[1].(map[string]any)["file"] = "02.md"
	chapters[1].(map[string]any)["lessons"] = []any{map[string]any{"id": "l04", "title": "04｜计划"}}
	files := outlineFiles(m)
	files["02.md"] = "# 02\n"
	return files
}

func TestFillingAnUpcomingLessonCountsAsAddingIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	subscribe(t, bob, id)
	before := lessonIDs(t, bob, id)

	admin.mustOK(updateCourse(t, admin, id, filledOutline()))

	after := lessonIDs(t, bob, id)
	if after["l02"] != before["l02"] {
		t.Fatalf("the filled lesson keeps its id: %v → %v", before, after)
	}
	bob.mustOK(lessonText(bob, id, after["l02"]))
	if p := courseProgress(t, bob, id); p["total"] != float64(3) {
		t.Fatalf("the filled lesson now counts: %v", p)
	}
	p := onlyNotification(t, bob, "course_updated")
	if p["added"] != float64(1) || p["removed"] != float64(0) || p["modified"] != float64(0) {
		t.Fatalf("filling l02 is one lesson added, the new upcoming l04 is none: %v", p)
	}
}

func TestAnUpdateRequestFillingAnUpcomingLessonShowsItAsAdded(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	bob.mustOK(submitUpdate(t, bob, id, filledOutline()))

	r := updateReviews(t, admin, id)[0]
	if c := r["changes"].(map[string]any); c["added"] != float64(1) || c["removed"] != float64(0) {
		t.Fatalf("filling l02 is one lesson added, the new upcoming l04 is none: %v", c)
	}
	expectChanges(t, r, "意图识别: 02｜三层意图识别[added]", "计划: 04｜计划[added] (upcoming)")
}
