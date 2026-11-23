package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func approve(admin *client, review map[string]any) resp {
	return admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(review["id"].(float64))), nil)
}

func TestApprovingADraftRequestKeepsTheUpdatesThatCameFirst(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l02", "title": "02｜意图"}))
	alice.mustOK(commitDraft(alice, id))
	// meanwhile an admin changes the course directly
	admin.mustOK(addOp(admin, id, map[string]any{"type": "renameChapter", "chapter": "intent", "title": "识别"}))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))
	admin.mustOK(commitDraft(admin, id))

	reviews := updateReviews(t, admin, id)
	expectChanges(t, reviews[0], "识别: 02｜意图[renamed]")
	admin.mustOK(approve(admin, reviews[0]))

	expectOutline(t, courseOutline(t, alice, id), "开篇:开篇词", "识别:01｜输入,02｜意图")
}

func TestADraftRequestFiledBeforeAnUpdateBringsItsUploadedLesson(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n\n![](c.png)\n", "c.png": "c-bytes"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜记忆", "content": map[string]any{"upload": u}}))
	key := draftKeys(t, alice, id)["03｜记忆"]
	alice.mustOK(commitDraft(alice, id))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "renameLesson", "lesson": "l00", "title": "前言"}))
	admin.mustOK(commitDraft(admin, id))

	approveUpdateOf(t, admin, id)

	expectOutline(t, courseOutline(t, bob, id), "开篇:前言", "意图识别:01｜用户输入规范化,02｜三层意图识别,03｜记忆")
	lesson := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, lessonIDs(t, bob, id)[key]), nil)).json()
	if lesson["markdown"] != "# 03\n\n![](c.png)\n" {
		t.Fatalf("the approved course reads the uploaded text: %v", lesson["markdown"])
	}
	if r := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/files/%s/c.png", id, u), nil)); string(r.body) != "c-bytes" {
		t.Fatalf("the approved course serves the uploaded image: %q", r.body)
	}
	filesIn(a, id) // the request's folder and the earlier versions are gone
}

func TestADraftRequestThatNoLongerAppliesCannotBeApproved(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))
	alice.mustOK(commitDraft(alice, id))
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l01"}))
	admin.mustOK(commitDraft(admin, id))
	before := courseOutline(t, alice, id)

	reviews := updateReviews(t, admin, id)
	if fmt.Sprint(conflicts(t, reviews[0])) != "[0:renameLesson:01｜用户输入规范化]" {
		t.Fatalf("the review lists what no longer applies: %v", reviews[0])
	}
	expectError(t, approve(admin, reviews[0]), http.StatusConflict, "request_conflicts")

	expectOutline(t, courseOutline(t, alice, id), before...)
	if n := len(updateReviews(t, admin, id)); n != 1 {
		t.Fatalf("the request stays to be turned down, got %d", n)
	}
}

func TestAMissingFileDoesNotHideTheReviews(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	u := uploadFiles(t, bob, id, map[string]string{"01.md": "# 01 新\n"})
	bob.mustOK(addOp(bob, id, map[string]any{"type": "replaceLesson", "lesson": "l01", "content": map[string]any{"upload": u}}))
	bob.mustOK(commitDraft(bob, id))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l02", "title": "02｜意图"}))
	alice.mustOK(commitDraft(alice, id))
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if strings.HasSuffix(k, "/"+u+"/01.md") || strings.HasSuffix(k, "/01_用户输入.md") {
			if err := a.files.Delete(context.Background(), k); err != nil {
				t.Fatal(err)
			}
		}
	}

	if n := len(updateReviews(t, admin, id)); n != 2 {
		t.Fatalf("both requests are listed, got %d", n)
	}
}

func TestADraftIsCommittedThoughAFileOfTheCourseIsMissing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	dir, _ := filesIn(a, id)
	if err := a.files.Delete(context.Background(), fmt.Sprintf("courses/%d/%s/00_开篇词.md", id, dir)); err != nil {
		t.Fatal(err)
	}
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))

	if r := alice.mustOK(commitDraft(alice, id)).json(); r["outcome"] != "updated" {
		t.Fatalf("the draft takes effect: %v", r)
	}
	expectOutline(t, courseOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜输入,02｜三层意图识别")
}

func TestUndoingTheOnlyOperationKeepsUploadsInProgress(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01"}))
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n"})

	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))

	alice.mustOK(putFile(alice, id, u, "c.png", "c-bytes"))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03", "content": map[string]any{"upload": u}}))
	if n := len(draftArea(a, id)); n != 2 {
		t.Fatalf("the upload keeps its files: %v", draftArea(a, id))
	}
}

func TestALessonZipMayNotUnpackPastTheUploadLimit(t *testing.T) {
	t.Parallel()
	a := newApp(t, withUploadLimit(1<<10))
	alice := a.user("alice")
	id := uploadCourse(t, alice, map[string]string{"course.json": manifestJSON(map[string]any{
		"version": 1, "title": "小课", "type": "column",
		"chapters": []any{map[string]any{"id": "c", "title": "章", "lessons": []any{map[string]any{"id": "l", "title": "讲", "file": "l.md"}}}},
	}), "l.md": "# l\n"})
	u := newUpload(t, alice, id)
	bomb := zipOf(t, map[string]string{"03.md": strings.Repeat("a", 4<<10)})

	expectError(t, putFile(alice, id, u, "03.zip", string(bomb)), http.StatusUnprocessableEntity, "draft_op_rejected")
	if n := len(draftArea(a, id)); n != 0 {
		t.Fatalf("nothing of it is stored: %v", draftArea(a, id))
	}
}
