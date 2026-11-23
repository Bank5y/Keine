package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// draftArea lists the files of a course's draft temporary areas.
func draftArea(a *app, id int) []string {
	a.t.Helper()
	var out []string
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if dir, _, _ := strings.Cut(strings.TrimPrefix(k, fmt.Sprintf("courses/%d/", id)), "/"); strings.HasPrefix(dir, "d") {
			out = append(out, k)
		}
	}
	return out
}

func TestAVideoLessonIsAddedFromAFileWithItsTranscript(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)
	u := uploadFiles(t, alice, id, map[string]string{"第二讲.mp4": "0123456789", "文字稿.md": "# 文字稿\n"})

	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "c1", "title": "第二讲", "content": map[string]any{"upload": u}}))

	l := draftLesson(t, alice, id, "第二讲")
	if l["video"] != "file" || l["hasText"] != true {
		t.Fatalf("the lesson has the video file and the transcript: %v", l)
	}
	p := alice.mustOK(preview(alice, id, l["key"].(string))).json()
	v := p["video"].(map[string]any)
	if v["mode"] != "player" || p["markdown"] != "# 文字稿\n" {
		t.Fatalf("the preview plays the uploaded video under its transcript: %v", p)
	}
	req, _ := http.NewRequest("GET", a.srv.URL+v["src"].(string), nil)
	req.Header.Set("Range", "bytes=2-4")
	if r := alice.send(req); r.status != http.StatusPartialContent || string(r.body) != "234" {
		t.Fatalf("the uploaded video can be scrubbed: %d %q", r.status, r.body)
	}

	alice.mustOK(commitDraft(alice, id))

	src := lessonVideo(t, alice, id, lessonIDs(t, alice, id)[l["key"].(string)])["src"].(string)
	if !strings.HasPrefix(src, fmt.Sprintf("/api/courses/%d/files/u-", id)) {
		t.Fatalf("the committed lesson plays the video from the course: %s", src)
	}
	if r := alice.mustOK(alice.do("GET", src, nil)); string(r.body) != "0123456789" {
		t.Fatalf("the committed video: %q", r.body)
	}
}

func TestAVideoLessonIsAddedFromALink(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)

	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "c1", "title": "外链", "content": map[string]any{"videoUrl": "https://www.bilibili.com/video/BV1xx411c7mD/"}}))

	l := draftLesson(t, alice, id, "外链")
	if l["video"] != "url" || l["hasText"] != false {
		t.Fatalf("the lesson plays the link: %v", l)
	}
	p := alice.mustOK(preview(alice, id, l["key"].(string))).json()
	if v := p["video"].(map[string]any); v["mode"] != "embed" {
		t.Fatalf("the preview embeds the link: %v", p)
	}
	expectError(t, addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "c1", "title": "x", "content": map[string]any{"videoUrl": "ftp://example.com/a.mp4"}}),
		http.StatusUnprocessableEntity, "draft_op_rejected")
	expectError(t, addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "c1", "title": "x", "content": map[string]any{"upload": uploadFiles(t, alice, id, map[string]string{"稿.md": "x"})}}),
		http.StatusUnprocessableEntity, "draft_op_rejected")

	alice.mustOK(commitDraft(alice, id))
	if v := lessonVideo(t, alice, id, lessonIDs(t, alice, id)[l["key"].(string)]); v["mode"] != "embed" {
		t.Fatalf("the committed lesson embeds the link: %v", v)
	}
}

func TestAColumnLessonMayCarryAVideoToo(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 实操\n", "demo.mp4": "video"})

	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜实操", "content": map[string]any{"upload": u}}))

	p := alice.mustOK(preview(alice, id, draftKeys(t, alice, id)["03｜实操"])).json()
	if v, _ := p["video"].(map[string]any); p["markdown"] != "# 实操\n" || v == nil || v["mode"] != "player" {
		t.Fatalf("the lesson has its text and its video: %v", p)
	}
}

func TestReplacingALessonKeepsItsProgressAndCarriesItsAnnotations(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)
	alice.mustOK(setProgress(alice, id, lesson, map[string]any{"done": true}))
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	revised := strings.Replace(openingText, "# 开篇词\n\n", "# 开篇词\n\n新的引言。\n\n", 1)
	u := uploadFiles(t, alice, id, map[string]string{"开篇词.md": revised, "a.png": "png-bytes"})

	alice.mustOK(addOp(alice, id, map[string]any{"type": "replaceLesson", "lesson": "l00", "content": map[string]any{"upload": u}}))

	if l := draftLesson(t, alice, id, "开篇词"); l["changed"] != true || l["added"] == true {
		t.Fatalf("the replaced lesson is marked changed: %v", l)
	}
	alice.mustOK(commitDraft(alice, id))

	if got := lessonIDs(t, alice, id)["l00"]; got != lesson {
		t.Fatalf("the replaced lesson keeps its id: %d, was %d", got, lesson)
	}
	if s := lessonStates(t, alice, id)["l00"]; s != "done" {
		t.Fatalf("the replaced lesson keeps its progress, got %s", s)
	}
	if text := alice.mustOK(lessonText(alice, id, lesson)).json(); text["markdown"] != revised {
		t.Fatalf("the lesson reads its new text: %v", text["markdown"])
	}
	expectAttached(t, annotationByID(t, listAnnotations(t, alice, id, lesson), ann), 25, 29)
	if pt := previousTexts(t, alice, id, lesson); len(pt) != 1 || pt[0]["markdown"] != openingText {
		t.Fatalf("the old text is kept for the annotations: %v", pt)
	}
}

func TestOnlyALessonWithContentIsReplacedAndOnlyAnUpcomingOneFilled(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, outlineFiles(outlineManifest()))
	text := func() map[string]any {
		return map[string]any{"upload": uploadFiles(t, alice, id, map[string]string{"02.md": "# 02\n"})}
	}

	expectError(t, addOp(alice, id, map[string]any{"type": "replaceLesson", "lesson": "l02", "content": text()}), http.StatusUnprocessableEntity, "draft_op_rejected")
	expectError(t, addOp(alice, id, map[string]any{"type": "uploadLessonContent", "lesson": "l01", "content": text()}), http.StatusUnprocessableEntity, "draft_op_rejected")
	before := lessonIDs(t, alice, id)

	alice.mustOK(addOp(alice, id, map[string]any{"type": "uploadLessonContent", "lesson": "l02", "content": text()}))
	alice.mustOK(commitDraft(alice, id))

	if got := lessonIDs(t, alice, id)["l02"]; got != before["l02"] {
		t.Fatalf("the filled lesson keeps its id: %d, was %d", got, before["l02"])
	}
	if text := alice.mustOK(lessonText(alice, id, before["l02"])).json(); text["markdown"] != "# 02\n" {
		t.Fatalf("the filled lesson can be read: %v", text)
	}
	if p := courseProgress(t, alice, id); p["total"] != float64(3) {
		t.Fatalf("the filled lesson counts in progress: %v", p)
	}
}

func TestAFilledLessonIsToldToSubscribersAsAdded(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	carol := a.user("carol")
	admin := a.admin()
	id := publishAs(t, a, outlineFiles(outlineManifest()))
	subscribe(t, carol, id)
	u := uploadFiles(t, admin, id, map[string]string{"02.md": "# 02\n"})

	admin.mustOK(addOp(admin, id, map[string]any{"type": "uploadLessonContent", "lesson": "l02", "content": map[string]any{"upload": u}}))
	admin.mustOK(commitDraft(admin, id))

	if p := onlyNotification(t, carol, "course_updated"); p["added"] != float64(1) || p["modified"] != float64(0) {
		t.Fatalf("filling an upcoming lesson counts as adding it: %v", p)
	}
}

func TestDiscardingADraftDeletesItsUploadedFiles(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03", "content": map[string]any{"upload": u}}))
	uploadFiles(t, alice, id, map[string]string{"04.md": "# 04\n"}) // not used yet
	if len(draftArea(a, id)) != 2 {
		t.Fatalf("uploads wait in the draft's area: %v", draftArea(a, id))
	}

	expectStatus(t, alice.do("DELETE", draftPath(id), nil), http.StatusNoContent)

	if left := draftArea(a, id); len(left) != 0 {
		t.Fatalf("discarding removes the uploaded files: %v", left)
	}
	expectError(t, putFile(alice, id, u, "03.md", "x"), http.StatusNotFound, "draft_upload_not_found")
}

func TestUndoingAnUploadDeletesItsFiles(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01"}))
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03", "content": map[string]any{"upload": u}}))

	alice.mustOK(alice.do("POST", draftPath(id)+"/undo", nil))

	if left := draftArea(a, id); len(left) != 0 {
		t.Fatalf("the undone lesson's files are removed: %v", left)
	}
	if n := len(draftOps(t, alice, id)); n != 1 {
		t.Fatalf("the earlier operation stays, got %d", n)
	}
}

func TestCommittingADraftDeletesItsTemporaryArea(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03", "content": map[string]any{"upload": u}}))

	alice.mustOK(commitDraft(alice, id))

	if left := draftArea(a, id); len(left) != 0 {
		t.Fatalf("the committed draft's area is removed: %v", left)
	}
}

func TestDeletingACourseDeletesItsDraftsFiles(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n"})

	expectStatus(t, deleteCourse(alice, id), http.StatusNoContent)

	if left := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(left) != 0 {
		t.Fatalf("a purged course leaves no files: %v", left)
	}
}

func TestDeletingACourseOthersRetainDeletesItsDraftsFiles(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, _, id, _ := subscriberWithDoneLesson(t, a)
	admin := a.admin()
	uploadFiles(t, admin, id, map[string]string{"03.md": "# 03\n"})

	expectStatus(t, deleteCourse(admin, id), http.StatusNoContent)

	if left := draftArea(a, id); len(left) != 0 {
		t.Fatalf("drafts of a deleted course go with their files: %v", left)
	}
}
