package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestADraftCommittedAfterAnotherUpdateKeepsBothChanges(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l02", "title": "02｜意图"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameChapter", "chapter": "intent", "title": "识别"}))
	_, files := nextVersion() // drops the opening, renames l01, adds a chapter
	alice.mustOK(updateCourse(t, alice, id, files))

	r := alice.mustOK(commitDraft(alice, id))

	if r.json()["outcome"] != "updated" {
		t.Fatalf("every operation still applies: %s", r.body)
	}
	expectOutline(t, courseOutline(t, alice, id), "识别:01｜用户输入规范化 (修订),02｜意图", "计划:03｜Plan")
}

// conflicts lists a draft's or a commit's conflicts as "index:type:target".
func conflicts(t *testing.T, body map[string]any) []string {
	t.Helper()
	var out []string
	list, _ := body["conflicts"].([]any)
	for _, c := range list {
		c := c.(map[string]any)
		op := c["op"].(map[string]any)
		target, _ := op["target"].(string)
		if m, _ := c["message"].(string); m == "" {
			t.Fatalf("a conflict says why: %v", c)
		}
		out = append(out, fmt.Sprintf("%v:%v:%s", c["index"], op["type"], target))
	}
	return out
}

// conflictIDs are the IDs of a draft's or a commit's conflicts, by which
// skipping them is confirmed.
func conflictIDs(body map[string]any) []string {
	var out []string
	list, _ := body["conflicts"].([]any)
	for _, c := range list {
		out = append(out, c.(map[string]any)["id"].(string))
	}
	return out
}

// conflictingDraft gives alice a draft of her course of which the first two
// operations no longer apply once the course is updated without its
// opening chapter.
func conflictingDraft(t *testing.T, alice *client) int {
	t.Helper()
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "前言"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "moveLesson", "lesson": "l02", "chapter": "opening", "after": ""}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameChapter", "chapter": "intent", "title": "识别"}))
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))
	return id
}

const bothConflicts = "[0:renameLesson:开篇词 1:moveLesson:02｜三层意图识别]"

func TestADraftShowsTheOperationsThatNoLongerApply(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := conflictingDraft(t, alice)

	d := getDraft(t, alice, id)

	if got := conflicts(t, d); fmt.Sprint(got) != bothConflicts {
		t.Fatalf("the draft lists its conflicts with what they were about: %v", got)
	}
	expectOutline(t, outlineOf(d["course"].(map[string]any)), "识别:01｜用户输入规范化 (修订),02｜三层意图识别", "计划:03｜Plan")
	if n := len(d["ops"].([]any)); n != 3 {
		t.Fatalf("the draft keeps every operation, got %d", n)
	}
}

func TestADraftWithConflictsStillTakesNewOperations(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := conflictingDraft(t, alice)

	d := alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l03", "title": "03｜计划"})).json()

	if fmt.Sprint(conflicts(t, d)) != bothConflicts {
		t.Fatalf("the conflicts stay listed: %v", d)
	}
	expectOutline(t, outlineOf(d["course"].(map[string]any)), "识别:01｜用户输入规范化 (修订),02｜三层意图识别", "计划:03｜计划")
	expectError(t, addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "x"}),
		http.StatusUnprocessableEntity, "draft_op_rejected")
	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))
	if n := len(draftOps(t, alice, id)); n != 3 {
		t.Fatalf("undo still drops the last operation, got %d", n)
	}
}

func TestCommittingADraftWithConflictsLeavesTheCourseUntilConfirmed(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := conflictingDraft(t, alice)
	before := courseOutline(t, alice, id)

	r := alice.mustOK(commitDraft(alice, id)).json()

	if r["outcome"] != "conflicts" || fmt.Sprint(conflicts(t, r)) != bothConflicts {
		t.Fatalf("the commit returns the conflicts: %v", r)
	}
	expectOutline(t, courseOutline(t, alice, id), before...)
	if n := len(draftOps(t, alice, id)); n != 3 {
		t.Fatalf("the draft stays, got %d operations", n)
	}

	r = alice.mustOK(alice.do("POST", draftPath(id)+"/commit", map[string]any{"skip": conflictIDs(r)})).json()

	if r["outcome"] != "updated" {
		t.Fatalf("confirmed conflicts are skipped: %v", r)
	}
	expectOutline(t, courseOutline(t, alice, id), "识别:01｜用户输入规范化 (修订),02｜三层意图识别", "计划:03｜Plan")
	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("a committed draft is gone, got %d operations", n)
	}
}

func TestOnlyTheConfirmedConflictsAreSkipped(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := conflictingDraft(t, alice)
	before := courseOutline(t, alice, id)
	first := conflictIDs(getDraft(t, alice, id))[:1]

	r := alice.mustOK(alice.do("POST", draftPath(id)+"/commit", map[string]any{"skip": first})).json()

	if r["outcome"] != "conflicts" || fmt.Sprint(conflicts(t, r)) != bothConflicts {
		t.Fatalf("a conflict not confirmed stops the commit: %v", r)
	}
	expectOutline(t, courseOutline(t, alice, id), before...)
}

func TestADraftCommittedAsAnUpdateRequestAlsoConfirmsItsConflicts(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "前言"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l00"}))
	admin.mustOK(commitDraft(admin, id))

	r := alice.mustOK(commitDraft(alice, id)).json()

	if r["outcome"] != "conflicts" || fmt.Sprint(conflicts(t, r)) != "[0:renameLesson:开篇词]" {
		t.Fatalf("the request is not filed with unconfirmed conflicts: %v", r)
	}
	if n := len(myUpdateRequests(t, alice, id)); n != 0 {
		t.Fatalf("no request was filed, got %d", n)
	}

	r = alice.mustOK(alice.do("POST", draftPath(id)+"/commit", map[string]any{"skip": conflictIDs(r)})).json()

	if r["outcome"] != "requested" {
		t.Fatalf("confirmed, the draft becomes an update request: %v", r)
	}
	if n := len(myUpdateRequests(t, alice, id)); n != 1 {
		t.Fatalf("one request was filed, got %d", n)
	}
}

func TestAConflictTakingTheSamePlaceIsNotSkippedUnseen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "前言"}))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l00"}))
	admin.mustOK(commitDraft(admin, id))
	seen := conflictIDs(alice.mustOK(commitDraft(alice, id)).json())
	// alice undoes the conflicting step and makes another, which the next
	// update makes a conflict in the same place
	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l01"}))
	admin.mustOK(commitDraft(admin, id))

	r := alice.mustOK(alice.do("POST", draftPath(id)+"/commit", map[string]any{"skip": seen})).json()

	if r["outcome"] != "conflicts" || fmt.Sprint(conflicts(t, r)) != "[0:renameLesson:01｜用户输入规范化]" {
		t.Fatalf("the new conflict is shown, not skipped: %v", r)
	}
	if n := len(myUpdateRequests(t, alice, id)); n != 0 {
		t.Fatalf("no request was filed, got %d", n)
	}
}

func TestADraftWhoseEveryOperationConflictsCannotBeCommitted(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "前言"}))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l00"}))
	admin.mustOK(commitDraft(admin, id))
	seen := conflictIDs(getDraft(t, alice, id))

	expectError(t, alice.do("POST", draftPath(id)+"/commit", map[string]any{"skip": seen}), http.StatusConflict, "draft_nothing_applies")

	if n := len(myUpdateRequests(t, alice, id)); n != 0 {
		t.Fatalf("no empty request is filed, got %d", n)
	}
	if n := len(draftOps(t, alice, id)); n != 1 {
		t.Fatalf("the draft stays, got %d operations", n)
	}
}
