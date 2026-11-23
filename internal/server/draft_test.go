package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func draftPath(id int) string { return fmt.Sprintf("/api/courses/%d/draft", id) }

// addOp appends one operation to the viewer's course draft.
func addOp(c *client, id int, op map[string]any) resp {
	return c.do("POST", draftPath(id)+"/ops", op)
}

// getDraft reads the viewer's course draft.
func getDraft(t *testing.T, c *client, id int) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", draftPath(id), nil)).json()
}

func draftOps(t *testing.T, c *client, id int) []any {
	t.Helper()
	ops, _ := getDraft(t, c, id)["ops"].([]any)
	return ops
}

// draftOutline is the draft's course as chapter titles, each with its
// lesson titles.
func draftOutline(t *testing.T, c *client, id int) []string {
	t.Helper()
	return outlineOf(getDraft(t, c, id)["course"].(map[string]any))
}

// courseOutline is the course as its readers see it, in the same form.
func courseOutline(t *testing.T, c *client, id int) []string {
	t.Helper()
	return outlineOf(courseJSON(t, c, id))
}

func outlineOf(d map[string]any) []string {
	var out []string
	for _, ch := range d["chapters"].([]any) {
		ch := ch.(map[string]any)
		line := ch["title"].(string) + ":"
		for i, l := range ch["lessons"].([]any) {
			if i > 0 {
				line += ","
			}
			line += l.(map[string]any)["title"].(string)
		}
		out = append(out, line)
	}
	return out
}

func expectOutline(t *testing.T, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("outline:\n got  %q\n want %q", got, want)
	}
}

// draftKeys maps the draft's chapter and lesson titles to their keys.
func draftKeys(t *testing.T, c *client, id int) map[string]string {
	t.Helper()
	keys := map[string]string{}
	for _, ch := range getDraft(t, c, id)["course"].(map[string]any)["chapters"].([]any) {
		ch := ch.(map[string]any)
		keys[ch["title"].(string)] = ch["key"].(string)
		for _, l := range ch["lessons"].([]any) {
			l := l.(map[string]any)
			keys[l["title"].(string)] = l["key"].(string)
		}
	}
	return keys
}

func TestADraftStartsEmptyAndCollectsOperations(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	d := getDraft(t, alice, id)
	if ops := d["ops"].([]any); len(ops) != 0 {
		t.Fatalf("a new draft has no operations: %v", ops)
	}
	expectOutline(t, draftOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")

	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": " 计划 "}))

	expectOutline(t, draftOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别", "计划:")
	if k := draftKeys(t, alice, id)["计划"]; k == "" {
		t.Fatal("a new chapter gets a key from the system")
	}
	expectOutline(t, courseOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
}

func TestADraftIsSeenOnlyByItsOwner(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	bob := a.user("bob")

	admin.mustOK(addOp(admin, id, map[string]any{"type": "addChapter", "title": "管理员的章"}))
	bob.mustOK(addOp(bob, id, map[string]any{"type": "addChapter", "title": "bob 的章"}))

	if n := len(draftOps(t, admin, id)); n != 1 {
		t.Fatalf("admin's draft should hold their one operation, got %d", n)
	}
	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("alice has no draft of her own, got %d operations", n)
	}
	expectOutline(t, draftOutline(t, bob, id)[2:], "bob 的章:")
}

func TestOnlyThoseWhoMayChangeACourseKeepADraft(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	private := uploadCourse(t, alice, columnFiles(columnManifest()))
	finished := publishedWith(t, a, alice, func(map[string]any) {})

	expectError(t, bob.do("GET", draftPath(private), nil), http.StatusNotFound, "course_not_found")
	expectError(t, addOp(bob, private, map[string]any{"type": "addChapter", "title": "x"}), http.StatusNotFound, "course_not_found")
	expectError(t, addOp(bob, finished, map[string]any{"type": "addChapter", "title": "x"}), http.StatusForbidden, "update_not_allowed")
}

func TestDraftOperationsReshapeTheCourse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["completion"] = "ongoing"
	id := uploadCourse(t, alice, columnFiles(m))
	op := func(o map[string]any) { t.Helper(); alice.mustOK(addOp(alice, id, o)) }

	op(map[string]any{"type": "renameChapter", "chapter": "intent", "title": "意图"})
	op(map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"})
	op(map[string]any{"type": "addChapter", "title": "计划"})
	plan := draftKeys(t, alice, id)["计划"]
	op(map[string]any{"type": "addUpcomingLesson", "chapter": plan, "title": "03｜Plan"})
	op(map[string]any{"type": "moveChapter", "chapter": plan, "after": ""})
	op(map[string]any{"type": "moveLesson", "lesson": "l02", "chapter": "intent", "after": ""})
	op(map[string]any{"type": "moveLesson", "lesson": "l00", "chapter": plan, "after": draftKeys(t, alice, id)["03｜Plan"]})
	op(map[string]any{"type": "deleteLesson", "lesson": "l01"})

	expectOutline(t, draftOutline(t, alice, id), "计划:03｜Plan,开篇词", "开篇:", "意图:02｜三层意图识别")
	if n := len(draftOps(t, alice, id)); n != 8 {
		t.Fatalf("the draft records each operation, got %d", n)
	}
	for _, ch := range getDraft(t, alice, id)["course"].(map[string]any)["chapters"].([]any) {
		ch := ch.(map[string]any)
		if ch["title"] == "计划" {
			up := ch["lessons"].([]any)[0].(map[string]any)
			if ch["added"] != true || up["added"] != true || up["upcoming"] != true {
				t.Fatalf("the new chapter and its upcoming lesson are marked as added: %v", ch)
			}
		}
	}
}

// A recorded lesson move names the chapter the lesson came from, so the
// editor can tell moving it to another chapter from reordering its own.
func TestALessonMoveRecordsTheChapterItCameFrom(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	op := func(o map[string]any) { t.Helper(); alice.mustOK(addOp(alice, id, o)) }

	op(map[string]any{"type": "moveLesson", "lesson": "l02", "chapter": "intent", "after": "", "from": "opening"})
	op(map[string]any{"type": "moveLesson", "lesson": "l00", "chapter": "intent", "after": ""})

	ops := draftOps(t, alice, id)
	if got := ops[0].(map[string]any)["from"]; got != "intent" {
		t.Fatalf("a move within a chapter records that chapter, not what the client sent: got %v", got)
	}
	if got := ops[1].(map[string]any)["from"]; got != "opening" {
		t.Fatalf("a move to another chapter records the chapter it left: got %v", got)
	}
}

func TestDraftChangesCourseInformation(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	alice.mustOK(addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{
		"title": "新课名", "completion": "incomplete", "intro": "新介绍", "authorName": "小明", "authorIntro": "讲师简介"}}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"title": "再改课名"}}))

	c := getDraft(t, alice, id)["course"].(map[string]any)
	if c["title"] != "再改课名" || c["completion"] != "incomplete" || c["intro"] != "新介绍" || c["authorName"] != "小明" || c["authorIntro"] != "讲师简介" {
		t.Fatalf("the draft's course should carry the new information: %v", c)
	}
	expectError(t, addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"title": "  "}}), http.StatusUnprocessableEntity, "draft_op_rejected")
	expectError(t, addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"completion": "done"}}), http.StatusUnprocessableEntity, "draft_op_rejected")
}

func errorMessage(r resp) string {
	e, _ := r.json()["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

func TestDeletingAChapterTakesItsLessonsButNeverTheLastChapter(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteChapter", "chapter": "intent"}))

	expectOutline(t, draftOutline(t, alice, id), "开篇:开篇词")
	expectError(t, addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "x"}), http.StatusUnprocessableEntity, "draft_op_rejected")
	r := addOp(alice, id, map[string]any{"type": "deleteChapter", "chapter": "opening"})
	expectError(t, r, http.StatusUnprocessableEntity, "draft_op_rejected")
	if !strings.Contains(errorMessage(r), "最后一章") {
		t.Fatalf("the user is told why: %s", r.body)
	}
	if n := len(draftOps(t, alice, id)); n != 1 {
		t.Fatalf("rejected operations are not recorded, got %d", n)
	}
}

func TestAFinishedCourseCannotGetUpcomingLessonsInADraft(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest())) // finished

	expectError(t, addOp(alice, id, map[string]any{"type": "addUpcomingLesson", "chapter": "intent", "title": "03"}), http.StatusUnprocessableEntity, "draft_op_rejected")
	alice.mustOK(addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"completion": "ongoing"}}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addUpcomingLesson", "chapter": "intent", "title": "03"}))

	r := addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"completion": "finished"}})
	expectError(t, r, http.StatusUnprocessableEntity, "draft_op_rejected")
	if !strings.Contains(errorMessage(r), "待更新") {
		t.Fatalf("the user is told why: %s", r.body)
	}
}

func TestBadOperationsAreRejected(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	for _, op := range []map[string]any{
		{"type": "explode"},
		{"type": "addChapter", "title": ""},
		{"type": "renameChapter", "chapter": "nope", "title": "x"},
		{"type": "moveChapter", "chapter": "intent"},
		{"type": "moveChapter", "chapter": "intent", "after": "nope"},
		{"type": "moveLesson", "lesson": "l01", "chapter": "opening", "after": "l02"},
		{"type": "deleteLesson", "lesson": "nope"},
		{"type": "setInfo"},
	} {
		expectError(t, addOp(alice, id, op), http.StatusUnprocessableEntity, "draft_op_rejected")
	}
	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("nothing should be recorded, got %d", n)
	}
}

func TestUndoGoesBackOneStepAtATimeToAnEmptyDraft(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameChapter", "chapter": "opening", "title": "序"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteLesson", "lesson": "l02"}))

	d := alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil)).json()
	expectOutline(t, outlineOf(d["course"].(map[string]any)), "序:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))

	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("undone to empty, got %d operations", n)
	}
	expectOutline(t, draftOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
	expectError(t, alice.do("POST", draftPath(id)+"/undo", nil), http.StatusConflict, "draft_empty")
}

func TestDiscardingADraftDropsEveryOperation(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteLesson", "lesson": "l02"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": "新章"}))

	expectStatus(t, alice.do("DELETE", draftPath(id), nil), http.StatusNoContent)

	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("a discarded draft is gone, got %d operations", n)
	}
	expectOutline(t, courseOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
	expectStatus(t, alice.do("DELETE", draftPath(id), nil), http.StatusNoContent)
}

func TestADraftSurvivesARestart(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "序言"}))

	again := a.restart().as("alice", "alice-password")

	expectOutline(t, draftOutline(t, again, id), "开篇:序言", "意图识别:01｜用户输入规范化,02｜三层意图识别")
}
