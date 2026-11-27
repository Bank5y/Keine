package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

// showIndex is the admin's view of the course's current index.
func showIndex(t *testing.T, admin *client, id int) map[string]any {
	t.Helper()
	return admin.mustOK(admin.do("GET", conceptIndexPath(id), nil)).json()
}

// indexLesson is a lesson of the index as the admin sees it.
func indexLesson(t *testing.T, x map[string]any, key string) map[string]any {
	t.Helper()
	for _, l := range x["lessons"].([]any) {
		if l := l.(map[string]any); l["key"] == key {
			return l
		}
	}
	t.Fatalf("no lesson %s in %v", key, x["lessons"])
	return nil
}

func retryPath(id int, key string) string {
	return fmt.Sprintf("/api/admin/courses/%d/concept-index/lessons/%s", id, key)
}

// listed is the course as the admin's list of indexes shows it.
func listed(t *testing.T, admin *client, id int) map[string]any {
	t.Helper()
	for _, x := range admin.mustOK(admin.do("GET", "/api/admin/concept-indexes", nil)).list("courses") {
		if c := x.(map[string]any); c["id"] == float64(id) {
			return c
		}
	}
	t.Fatalf("course %d not listed", id)
	return nil
}

func TestALessonThatFailsLeavesTheOthersToFinishAndTheIndexPartial(t *testing.T) {
	t.Parallel()
	steps := threeLessons()
	m := newScriptedModel(steps[0], of(l01, texts("这一讲没有概念可言。")), steps[2])
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))

	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))

	if s := awaitIndex(t, admin, id); s != "partial" {
		t.Fatalf("with one lesson failing, the index is %q", s)
	}
	x := showIndex(t, admin, id)
	if l := indexLesson(t, x, "l01"); l["status"] != "failed" || l["error"] != "malformed_output" {
		t.Fatalf("the failed lesson has its reason: %v", l)
	}
	for _, k := range []string{"l00", "l02"} {
		if l := indexLesson(t, x, k); l["status"] != "done" || l["entries"] != 1.0 {
			t.Fatalf("lesson %s is extracted: %v", k, l)
		}
	}
	if p := x["progress"].(map[string]any); p["total"] != 3.0 || p["done"] != 2.0 || p["failed"] != 1.0 {
		t.Fatalf("progress = %v", p)
	}
	if c := listed(t, admin, id); c["failed"] != 1.0 || c["entries"] != 2.0 {
		t.Fatalf("the list counts the failed lesson: %v", c)
	}
	// a partial index still links by what it has
	if f := fused(t, recallTest(t, admin, id, "Agent 和意图识别", "")); len(f) != 2 {
		t.Fatalf("a partial index recalls its concepts: %v", f)
	}
}

func TestRetryingALessonExtractsItAloneAndBuildsTheIndex(t *testing.T) {
	t.Parallel()
	steps := threeLessons()
	m := newScriptedModel(steps[0], of(l01, texts("这一讲没有概念可言。")), steps[2], steps[1])
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	r := admin.mustOK(admin.do("POST", retryPath(id, "l01"), nil))

	if r.status != http.StatusAccepted {
		t.Fatalf("retrying: %d %s", r.status, r.body)
	}
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("after the retry, the index is %q", s)
	}
	calls := m.calls()
	if len(calls) != 4 {
		t.Fatalf("the retry extracts that lesson alone: %d calls in all", len(calls))
	}
	extractionOf(t, calls[3:], l01)
	if l := indexLesson(t, showIndex(t, admin, id), "l01"); l["status"] != "done" || l["error"] != nil {
		t.Fatalf("the retried lesson: %v", l)
	}
	if e := indexEntries(t, admin, id); len(e) != 3 {
		t.Fatalf("entries = %v", e)
	}
	expectError(t, admin.do("POST", retryPath(id, "l99"), nil), http.StatusNotFound, "index_lesson_not_found")
}

func TestWhileBuildingTheAdminSeesEachLessonsProgress(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	steps := threeLessons()
	steps[1].before = func() { <-release }
	m := newScriptedModel(steps...)
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))

	var x map[string]any
	for range 500 {
		x = showIndex(t, admin, id)
		if x["progress"].(map[string]any)["done"] == 2.0 {
			break
		}
	}
	close(release)

	if x["status"] != "building" || x["progress"].(map[string]any)["done"] != 2.0 {
		t.Fatalf("two of three lessons done while building: %v", x)
	}
	if l := indexLesson(t, x, "l01"); l["status"] != "running" {
		t.Fatalf("the held lesson is running: %v", l)
	}
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("the index is %q", s)
	}
}

func TestAnIndexOfAnotherModelIsStaleAndRebuildingExtractsAgain(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(append(threeLessons(), threeLessons()...)...)
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)
	if x := showIndex(t, admin, id); x["stale"] != false || x["modelName"] != "Flash" || x["promptVersion"] != "concept-index-2" {
		t.Fatalf("a fresh index: %v", x)
	}

	other := admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-test-0123456789", "model": "m-2", "displayName": "Pro", "rates": rates, "searchRoute": "off",
	})).json()
	admin.mustOK(admin.do("PUT", "/api/admin/turns/concept_index", map[string]any{"modelId": other["id"]}))

	if x := showIndex(t, admin, id); x["stale"] != true || x["status"] != "built" {
		t.Fatalf("extracted by another model, the index is stale and still built: %v", x)
	}
	if c := listed(t, admin, id); c["stale"] != true {
		t.Fatalf("the list shows it stale: %v", c)
	}
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("rebuilt, the index is %q", s)
	}
	if n := len(m.calls()); n != 6 {
		t.Fatalf("no lesson of the other model is kept: %d calls in all", n)
	}
	if x := showIndex(t, admin, id); x["stale"] != false || x["modelName"] != "Pro" {
		t.Fatalf("rebuilt: %v", x)
	}
}

func TestRebuildingKeepsTheLessonsOfTheSamePromptAndModel(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(threeLessons()...)
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))

	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("rebuilt, the index is %q", s)
	}
	if n := len(m.calls()); n != 3 {
		t.Fatalf("nothing is extracted again: %d calls", n)
	}
	if l := indexLesson(t, showIndex(t, admin, id), "l00"); l["reused"] != true || l["status"] != "done" {
		t.Fatalf("a lesson kept: %v", l)
	}
}

// asIfV1 makes the course's index look as one extracted before #224:
// with the first prompt, by a model not recorded.
func asIfV1(t *testing.T, a *app, id int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{
		"UPDATE concept_indexes SET prompt_version = 'concept-index-1', model = '', model_name = '' WHERE course_id = $1",
		"UPDATE concept_index_lessons SET prompt_version = 'concept-index-1', model = '' WHERE index_id IN (SELECT id FROM concept_indexes WHERE course_id = $1)",
	} {
		if _, err := conn.Exec(ctx, q, id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAV1IndexIsStaleButLinksUntilRebuilt(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(append(threeLessons(), threeLessons()...)...)
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	asIfV1(t, a, id)

	if x := showIndex(t, admin, id); x["stale"] != true || x["status"] != "built" || x["promptVersion"] != "concept-index-1" {
		t.Fatalf("a v1 index: %v", x)
	}
	if c := recallPathHits(t, recallTest(t, admin, id, "意图识别", ""), "concept"); len(c) != 1 {
		t.Fatalf("a v1 index still recalls its concepts: %v", c)
	}
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)
	if n := len(m.calls()); n != 6 {
		t.Fatalf("no lesson of the first prompt is kept: %d calls in all", n)
	}
	if x := showIndex(t, admin, id); x["stale"] != false {
		t.Fatalf("rebuilt: %v", x)
	}
}
