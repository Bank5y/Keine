package server_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// The text of annotatedColumn's opening lesson (l00).
const openingText = "# 开篇词\n\n" +
	"Agent 是一个**会思考**的程序，它会[调用工具](https://example.com)完成任务。\n\n" +
	"第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n" +
	"```go\nfunc main() { run() }\n```\n\n" +
	"![示意图](images/1/a.png)\n\n" +
	"最后一段再说一次 Agent。\n"

// annotatedFiles is annotatedColumn's package with the opening lesson's
// text replaced by opening.
func annotatedFiles(opening string) map[string]string {
	files := columnFiles(columnManifest())
	files["00_开篇词.md"] = opening
	return files
}

// publishedAnnotatedColumn publishes annotatedColumn, uploaded by alice.
func publishedAnnotatedColumn(t *testing.T, a *app, alice *client) (courseID int, lessons map[string]int) {
	t.Helper()
	courseID = uploadCourse(t, alice, annotatedFiles(openingText))
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", courseID), nil))
	return courseID, lessonIDs(t, alice, courseID)
}

// approveUpdate has c request an update to files and the admin approve it.
func approveUpdate(t *testing.T, a *app, c *client, id int, files map[string]string) {
	t.Helper()
	c.mustOK(submitUpdate(t, c, id, files))
	admin := a.admin()
	reviews := updateReviews(t, admin, id)
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(reviews[len(reviews)-1]["id"].(float64))), nil))
}

func previousTexts(t *testing.T, c *client, courseID, lessonID int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, v := range c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d/previous-texts", courseID, lessonID), nil)).list("previousTexts") {
		out = append(out, v.(map[string]any))
	}
	return out
}

func annotationByID(t *testing.T, list []map[string]any, ann map[string]any) map[string]any {
	t.Helper()
	for _, a := range list {
		if a["id"] == ann["id"] {
			return a
		}
	}
	t.Fatalf("annotation %v not in %v", ann["id"], list)
	return nil
}

func expectAttached(t *testing.T, a map[string]any, start, end int) {
	t.Helper()
	if rg := textRange(a); a["state"] != "attached" || rg == nil || rg[0] != start || rg[1] != end || a["previousTextId"] != nil {
		t.Fatalf("expected an annotation attached at [%d,%d), got %v", start, end, a)
	}
}

func TestAnnotationsOnAnUnchangedLessonStayAsTheyWere(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	ann := annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	expectAttached(t, ann, 20, 24)

	files := annotatedFiles(openingText)
	files["01_用户输入.md"] = "# 01\n\n全新的内容。\n"
	a.admin().mustOK(updateCourse(t, a.admin(), id, files))

	expectAttached(t, annotationByID(t, listAnnotations(t, bob, id, lessons["l00"]), ann), 20, 24)
	if pt := previousTexts(t, bob, id, lessons["l00"]); len(pt) != 0 {
		t.Fatalf("an unchanged lesson keeps no previous text: %v", pt)
	}
}

func TestAnnotationsFollowTheirTextWhenOtherParagraphsChange(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	ann := annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	last := annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "Agent", "prefix": "最后一段再说一次 ", "suffix": "。"})

	// a new paragraph of 5 characters up front; the code block is rewritten
	revised := strings.Replace(openingText, "# 开篇词\n\n", "# 开篇词\n\n新的引言。\n\n", 1)
	revised = strings.Replace(revised, "func main() { run() }", "func main() { start() }", 1)
	approveUpdate(t, a, alice, id, annotatedFiles(revised))

	got := listAnnotations(t, bob, id, lessons["l00"])
	expectAttached(t, annotationByID(t, got, ann), 25, 29)
	expectAttached(t, annotationByID(t, got, last), 89, 94)

	pt := previousTexts(t, bob, id, lessons["l00"])
	if len(pt) != 1 || pt[0]["markdown"] != openingText || pt[0]["replacedAt"] == nil {
		t.Fatalf("expected the old text kept, got %v", pt)
	}
}

func TestAnnotationsWhoseTextChangedAwaitMigration(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	l00 := lessons["l00"]
	edited := annotate(t, bob, id, l00, map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	removed := annotate(t, bob, id, l00, map[string]any{"style": "highlight", "color": "blue",
		"quote": "Agent", "prefix": "第二段也提到 ", "suffix": "，以及 tool_call"})
	kept := annotate(t, bob, id, l00, map[string]any{"style": "underline", "quote": "会思考", "prefix": "Agent 是一个", "suffix": "的程序"})

	// the tool sentence is reworded and the second paragraph is gone; the
	// other two Agents stay, but neither is the annotated one
	revised := strings.Replace(openingText, "[调用工具](https://example.com)", "使用工具", 1)
	revised = strings.Replace(revised, "第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n", "", 1)
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(revised)))

	pt := previousTexts(t, bob, id, l00)
	if len(pt) != 1 || pt[0]["markdown"] != openingText {
		t.Fatalf("expected the old text kept, got %v", pt)
	}
	got := listAnnotations(t, bob, id, l00)
	for _, ann := range []map[string]any{edited, removed} {
		g := annotationByID(t, got, ann)
		if g["state"] != "pending" || g["range"] != nil || g["previousTextId"] != pt[0]["id"] || g["quote"] != ann["quote"] {
			t.Fatalf("expected a pending annotation pointing at the old text, got %v", g)
		}
	}
	expectAttached(t, annotationByID(t, got, kept), 11, 14)

	// a second update leaves them pending, still against the text they were
	// made on, and a later text holding the quote again takes it back
	back := strings.Replace(openingText, "第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n", "", 1)
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(back)))
	got = listAnnotations(t, bob, id, l00)
	if g := annotationByID(t, got, removed); g["state"] != "pending" || g["previousTextId"] != pt[0]["id"] {
		t.Fatalf("expected still pending against the first old text, got %v", g)
	}
	expectAttached(t, annotationByID(t, got, edited), 20, 24)
	if pt := previousTexts(t, bob, id, l00); len(pt) != 2 || pt[0]["markdown"] != revised || pt[1]["markdown"] != openingText {
		t.Fatalf("expected both old texts, newest first, got %v", pt)
	}
}

func TestAnnotationsOfADeletedLessonBecomeExcerpts(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "会思考"})
	subscribe(t, bob, id)

	approveUpdate(t, a, alice, id, revisedVersion()) // drops l00

	anns, _ := myAnnotations(t, bob, url.Values{})
	if len(anns) != 2 {
		t.Fatalf("expected both annotations on the notes page, got %v", anns)
	}
	for _, n := range anns {
		if n["state"] != "deleted" || n["lesson"] != nil || n["range"] != nil {
			t.Fatalf("expected an excerpt whose text was deleted, got %v", n)
		}
	}
}

func TestNotesPageShowsPendingAnnotations(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "调用工具", "prefix": "它会", "suffix": "完成任务"})
	annotate(t, bob, id, lessons["l00"], map[string]any{"style": "underline", "quote": "会思考", "prefix": "Agent 是一个", "suffix": "的程序"})

	revised := strings.Replace(openingText, "[调用工具](https://example.com)", "使用工具", 1)
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(revised)))

	anns, _ := myAnnotations(t, bob, url.Values{"sort": {"text"}})
	if len(anns) != 2 || anns[0]["quote"] != "会思考" || anns[0]["state"] != "attached" || textRange(anns[0]) == nil ||
		anns[1]["quote"] != "调用工具" || anns[1]["state"] != "pending" || anns[1]["range"] != nil || anns[1]["lesson"] == nil {
		t.Fatalf("unexpected notes: %v", anns)
	}
}

func TestPreviousTextsAreReadOnlyByWhoeverMayReadTheLesson(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := uploadCourse(t, alice, annotatedFiles(openingText))
	l00 := lessonIDs(t, alice, id)["l00"]
	alice.mustOK(updateCourse(t, alice, id, annotatedFiles("# 开篇词\n\n改了。\n")))

	if pt := previousTexts(t, alice, id, l00); len(pt) != 1 || pt[0]["markdown"] != openingText {
		t.Fatalf("expected the uploader to read the old text, got %v", pt)
	}
	r := bob.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d/previous-texts", id, l00), nil)
	if r.status != 404 {
		t.Fatalf("another user's private course stays hidden, got %d", r.status)
	}
}

func TestAnEmptyTextThatChangedIsKeptToo(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, annotatedFiles(""))
	l00 := lessonIDs(t, alice, id)["l00"]
	alice.mustOK(updateCourse(t, alice, id, annotatedFiles(openingText)))

	if pt := previousTexts(t, alice, id, l00); len(pt) != 1 || pt[0]["markdown"] != "" {
		t.Fatalf("expected the empty old text to be kept, got %v", pt)
	}
}
