package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
)

// indexedFiles is the column course with a section in every lesson, for
// the concept index to place its entries in.
func indexedFiles() map[string]string {
	files := columnFiles(columnManifest())
	files["00_开篇词.md"] = "# 开篇词\n\n## 为什么学 Agent\n\nAgent 是能自己规划步骤的程序。\n"
	files["01_用户输入.md"] = "# 01\n\n## 规范化\n\n输入规范化把用户的话整理成统一的形式。\n"
	files["02_意图识别.md"] = "# 02\n\n## 三层意图\n\n意图识别分三层：领域、意图、槽位。\n"
	return files
}

// concepts is a model's reply for one lesson: its concepts as JSON.
func concepts(entries ...map[string]any) step {
	b, _ := json.Marshal(entries)
	return texts(string(b))
}

func concept(name, section, summary string, aliases ...string) map[string]any {
	if aliases == nil {
		aliases = []string{}
	}
	return map[string]any{"name": name, "aliases": aliases, "section": section, "summary": summary}
}

// The titles of the column course's lessons.
const (
	l00 = "开篇词"
	l01 = "01｜用户输入规范化"
	l02 = "02｜三层意图识别"
)

// of keeps a step for the extraction of the lesson titled title.
func of(title string, s step) step {
	s.lesson = title
	return s
}

// threeLessons are the replies for the three lessons of indexedFiles.
func threeLessons() []step {
	return []step{
		of(l00, concepts(concept("Agent", "为什么学 Agent", "能自己规划步骤的程序。", "智能体"))),
		of(l01, concepts(concept("输入规范化", "规范化", "把用户的话整理成统一的形式。"))),
		of(l02, concepts(concept("意图识别", "三层意图", "分领域、意图、槽位三层。", "Intent"))),
	}
}

func conceptIndexPath(id int) string { return fmt.Sprintf("/api/admin/courses/%d/concept-index", id) }

// indexStatus is the status the admin page lists for a course.
func indexStatus(t *testing.T, admin *client, id int) string {
	t.Helper()
	for _, x := range admin.mustOK(admin.do("GET", "/api/admin/concept-indexes", nil)).list("courses") {
		c := x.(map[string]any)
		if c["id"] == float64(id) {
			return c["status"].(string)
		}
	}
	t.Fatalf("course %d not listed", id)
	return ""
}

// awaitIndex waits for the course's index to stop building.
func awaitIndex(t *testing.T, admin *client, id int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := indexStatus(t, admin, id); s != "building" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the index is still building")
	return ""
}

// indexEntries are the entries of the course's current index.
func indexEntries(t *testing.T, admin *client, id int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range admin.mustOK(admin.do("GET", conceptIndexPath(id), nil)).list("entries") {
		out = append(out, x.(map[string]any))
	}
	return out
}

func entryNamed(t *testing.T, entries []map[string]any, name string) map[string]any {
	t.Helper()
	for _, e := range entries {
		if e["name"] == name {
			return e
		}
	}
	t.Fatalf("no entry %q in %v", name, entries)
	return nil
}

func publishedIndexed(t *testing.T, a *app, alice *client) int {
	t.Helper()
	id := uploadCourse(t, alice, indexedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	return id
}

func TestANewVersionStartsWithoutAnIndexAndNothingIsExtracted(t *testing.T) {
	t.Parallel()
	m := newScriptedModel()
	a := aiApp(t, m)
	alice := a.user("alice")
	admin := a.admin()

	id := publishedIndexed(t, a, alice)

	if s := indexStatus(t, admin, id); s != "none" {
		t.Fatalf("a new version's index is %q", s)
	}
	if n := len(m.calls()); n != 0 {
		t.Fatalf("the model was called %d times without the admin asking", n)
	}
	expectStatus(t, admin.do("GET", conceptIndexPath(id), nil), http.StatusOK)
	if e := indexEntries(t, admin, id); len(e) != 0 {
		t.Fatalf("entries before the index is built: %v", e)
	}
}

func TestTheAdminBuildsTheIndexLessonByLesson(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	steps := threeLessons()
	steps[0].before = func() { <-release }
	m := newScriptedModel(steps...)
	a := aiApp(t, m)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedIndexed(t, a, alice)

	r := admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if r.status != http.StatusAccepted || r.json()["status"] != "building" {
		t.Fatalf("starting the index: %d %s", r.status, r.body)
	}
	if s := indexStatus(t, admin, id); s != "building" {
		t.Fatalf("while extracting, the index is %q", s)
	}
	expectError(t, admin.do("POST", conceptIndexPath(id), nil), http.StatusConflict, "index_building")
	close(release)

	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("after extracting, the index is %q", s)
	}
	calls := m.calls()
	if len(calls) != 3 {
		t.Fatalf("one extraction a lesson, got %d calls", len(calls))
	}
	if last := extractionOf(t, calls, l01); !strings.Contains(last, "输入规范化把用户的话整理成统一的形式") || strings.Contains(last, "意图识别分三层") {
		t.Fatalf("an extraction reads its own lesson only: %q", last)
	}
	if len(calls[0].Tools) != 0 {
		t.Fatalf("an extraction reads the lesson it is given, with no tools: %v", calls[0].Tools)
	}

	entries := indexEntries(t, admin, id)
	if len(entries) != 3 {
		t.Fatalf("entries = %v", entries)
	}
	agent := entryNamed(t, entries, "Agent")
	lesson := agent["lesson"].(map[string]any)
	section := agent["section"].(map[string]any)
	if lesson["key"] != "l00" || lesson["title"] != "开篇词" || lesson["id"] != float64(lessonIDs(t, alice, id)["l00"]) ||
		section["title"] != "为什么学 Agent" || section["id"] != "为什么学-agent" ||
		agent["summary"] != "能自己规划步骤的程序。" || fmt.Sprint(agent["aliases"]) != "[智能体]" {
		t.Fatalf("entry = %v", agent)
	}
}

// extractionOf is the request of the extraction of the lesson titled title.
func extractionOf(t *testing.T, calls []assistant.Request, title string) string {
	t.Helper()
	for _, c := range calls {
		if last := c.Messages[len(c.Messages)-1].Content; strings.Contains(last, "› "+title+"\n") {
			return last
		}
	}
	t.Fatalf("no extraction of %s", title)
	return ""
}

func TestANewVersionExtractsOnlyTheLessonsThatChanged(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(append(threeLessons(),
		of(l01, concepts(concept("输入清洗", "清洗", "去掉无关字符。"))))...)
	a := aiApp(t, m)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedIndexed(t, a, alice)
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	files := indexedFiles()
	files["01_用户输入.md"] = "# 01\n\n## 清洗\n\n输入清洗去掉无关字符。\n"
	expectStatus(t, submitUpdate(t, alice, id, files), http.StatusCreated)
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%v/approve", updateReviews(t, admin, id)[0]["id"]), nil))

	if s := indexStatus(t, admin, id); s != "none" {
		t.Fatalf("the new version's index is %q", s)
	}
	if n := len(m.calls()); n != 3 {
		t.Fatalf("a new version is not indexed on its own: %d calls", n)
	}
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("the new version's index is %q", s)
	}

	calls := m.calls()
	if len(calls) != 4 {
		t.Fatalf("only the changed lesson is extracted: %d calls in all", len(calls))
	}
	if last := calls[3].Messages[len(calls[3].Messages)-1].Content; !strings.Contains(last, "输入清洗去掉无关字符") {
		t.Fatalf("the extraction is of another lesson: %q", last)
	}
	entries := indexEntries(t, admin, id)
	if len(entries) != 3 {
		t.Fatalf("entries = %v", entries)
	}
	entryNamed(t, entries, "Agent")
	entryNamed(t, entries, "意图识别")
	entryNamed(t, entries, "输入清洗")
}

func TestTheIndexIsChargedToTheSiteNotToAReader(t *testing.T) {
	t.Parallel()
	steps := threeLessons()
	for i := range steps {
		steps[i].result.Usage = assistant.Usage{Input: n64(1_000_000), Output: n64(0)}
	}
	m := newScriptedModel(steps...)
	a := aiApp(t, m)
	alice := a.user("alice")
	onPlan(t, a, alice, 100)
	admin := a.admin()
	id := publishedIndexed(t, a, alice)

	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	site := admin.mustOK(admin.do("GET", "/api/admin/usage", nil)).json()["total"].(map[string]any)
	if site["runs"] != 3.0 || !near(site["credits"], 6) {
		t.Fatalf("the site's usage = %v", site)
	}
	if readers := admin.mustOK(admin.do("GET", "/api/admin/usage/users", nil)).list("users"); len(readers) != 0 {
		t.Fatalf("the index is charged to a reader: %v", readers)
	}
	if cr := credits(t, alice); !near(cr["remaining"], 100) {
		t.Fatalf("the uploader's credits = %v", cr)
	}
}

func TestOnlyTheAdminBuildsAnIndexOfALibraryCourse(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	alice := a.user("alice")
	admin := a.admin()
	private := uploadCourse(t, alice, indexedFiles())

	expectStatus(t, alice.do("POST", conceptIndexPath(private), nil), http.StatusForbidden)
	expectError(t, admin.do("POST", conceptIndexPath(private), nil), http.StatusNotFound, "course_not_found")
	expectError(t, admin.do("POST", conceptIndexPath(99999), nil), http.StatusNotFound, "course_not_found")
}

func TestALessonMissingFromStorageIsLeftOutOfTheIndex(t *testing.T) {
	t.Parallel()
	steps := threeLessons()
	m := newScriptedModel(steps[1], steps[2])
	a := aiApp(t, m)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedIndexed(t, a, alice)
	dir, _ := filesIn(a, id)
	if err := a.files.Delete(context.Background(), fmt.Sprintf("courses/%d/%s/00_开篇词.md", id, dir)); err != nil {
		t.Fatal(err)
	}

	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("with one lesson's text missing, the index is %q", s)
	}
	if n := len(m.calls()); n != 2 {
		t.Fatalf("only the lessons with a text are extracted: %d calls", n)
	}
	entries := indexEntries(t, admin, id)
	if len(entries) != 2 {
		t.Fatalf("entries = %v", entries)
	}
	entryNamed(t, entries, "意图识别")
}
