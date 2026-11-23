package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// annotatedColumn is a column whose first text lesson has room for
// annotations: repeated words, inline formatting and a code block.
func annotatedColumn(t *testing.T, c *client) (courseID, lessonID int) {
	t.Helper()
	files := columnFiles(columnManifest())
	files["00_开篇词.md"] = "# 开篇词\n\n" +
		"Agent 是一个**会思考**的程序，它会[调用工具](https://example.com)完成任务。\n\n" +
		"第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n" +
		"```go\nfunc main() { run() }\n```\n\n" +
		"![示意图](images/1/a.png)\n\n" +
		"最后一段再说一次 Agent。\n"
	courseID = uploadCourse(t, c, files)
	return courseID, lessonIDs(t, c, courseID)["l00"]
}

func annotationsPath(courseID, lessonID int) string {
	return fmt.Sprintf("/api/courses/%d/lessons/%d/annotations", courseID, lessonID)
}

func listAnnotations(t *testing.T, c *client, courseID, lessonID int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, v := range c.mustOK(c.do("GET", annotationsPath(courseID, lessonID), nil)).list("annotations") {
		out = append(out, v.(map[string]any))
	}
	return out
}

// textRange is where an annotation sits in the lesson text, counted in
// non-whitespace characters; nil when the text no longer has it.
func textRange(a map[string]any) []int {
	r, ok := a["range"].(map[string]any)
	if !ok {
		return nil
	}
	return []int{int(r["start"].(float64)), int(r["end"].(float64))}
}

func TestAnnotationIsSavedAndFoundInTheText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	r := alice.do("POST", annotationsPath(id, lesson), map[string]any{
		"style": "highlight", "color": "yellow", "quote": "会思考", "prefix": "Agent 是一个", "suffix": "的程序",
	})
	expectStatus(t, r, http.StatusCreated)
	created := r.json()

	got := listAnnotations(t, alice, id, lesson)
	if len(got) != 1 {
		t.Fatalf("expected one annotation, got %v", got)
	}
	ann := got[0]
	if ann["id"] != created["id"] || ann["style"] != "highlight" || ann["color"] != "yellow" ||
		ann["quote"] != "会思考" || ann["prefix"] != "Agent 是一个" || ann["suffix"] != "的程序" || ann["note"] != nil {
		t.Fatalf("unexpected annotation: %v", ann)
	}
	// "开篇词" + "Agent是一个" = 3 + 8 non-whitespace characters before it
	if rg := textRange(ann); rg == nil || rg[0] != 11 || rg[1] != 14 {
		t.Fatalf("unexpected range: %v", ann["range"])
	}
}

func annotate(t *testing.T, c *client, courseID, lessonID int, body map[string]any) map[string]any {
	t.Helper()
	r := c.do("POST", annotationsPath(courseID, lessonID), body)
	expectStatus(t, r, http.StatusCreated)
	return r.json()
}

// The visible text of annotatedColumn, whitespace removed, with offsets:
//
//	0 开篇词 3 Agent是一个 11 会思考 14 的程序，它会 20 调用工具 24 完成任务。 29
//	第二段也提到 35 Agent 40 ，以及 43 tool_call 52 这个字段。 57
//	funcmain(){ 68 run() 73 } 74 最后一段再说一次 82 Agent 87 。

func TestRepeatedTextIsToldApartByItsSurroundings(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "Agent", "prefix": "最后一段再说一次 ", "suffix": "。"})

	if rg := textRange(ann); rg == nil || rg[0] != 82 || rg[1] != 87 {
		t.Fatalf("unexpected range: %v", ann["range"])
	}
}

func TestSelectionsMayCrossParagraphsAndInlineFormatting(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	// from bold text, through a link and a paragraph break, into inline code
	ann := annotate(t, alice, id, lesson, map[string]any{
		"style": "highlight", "color": "green",
		"quote":  "思考的程序，它会调用工具完成任务。\n\n第二段也提到 Agent，以及 tool_",
		"prefix": "是一个会", "suffix": "call",
	})
	if rg := textRange(ann); rg == nil || rg[0] != 12 || rg[1] != 48 {
		t.Fatalf("unexpected range: %v", ann["range"])
	}
	// text in a code block can be annotated too
	code := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "run()", "prefix": "func main() { ", "suffix": " }"})
	if rg := textRange(code); rg == nil || rg[0] != 68 || rg[1] != 73 {
		t.Fatalf("unexpected range: %v", code["range"])
	}
}

func annotationPath(a map[string]any) string {
	return fmt.Sprintf("/api/annotations/%d", int(a["id"].(float64)))
}

func TestAnnotationsAreRestyledAndDeleted(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具"})

	expectStatus(t, alice.do("PATCH", annotationPath(ann), map[string]any{"style": "highlight", "color": "pink"}), http.StatusNoContent)
	got := listAnnotations(t, alice, id, lesson)
	if len(got) != 1 || got[0]["style"] != "highlight" || got[0]["color"] != "pink" || got[0]["quote"] != "调用工具" {
		t.Fatalf("unexpected annotations: %v", got)
	}
	expectStatus(t, alice.do("PATCH", annotationPath(ann), map[string]any{"style": "underline"}), http.StatusNoContent)
	if got := listAnnotations(t, alice, id, lesson); got[0]["style"] != "underline" || got[0]["color"] != nil {
		t.Fatalf("unexpected annotations: %v", got)
	}

	expectStatus(t, alice.do("DELETE", annotationPath(ann), nil), http.StatusNoContent)
	if got := listAnnotations(t, alice, id, lesson); len(got) != 0 {
		t.Fatalf("expected no annotations, got %v", got)
	}
	expectError(t, alice.do("DELETE", annotationPath(ann), nil), http.StatusNotFound, "annotation_not_found")
}

func TestAnnotationStylesAreUnderlineOrAHighlightColour(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)
	for _, body := range []map[string]any{
		{"style": "strike", "quote": "调用工具"},
		{"style": "highlight", "quote": "调用工具"},
		{"style": "highlight", "color": "red", "quote": "调用工具"},
		{"style": "underline", "color": "yellow", "quote": "调用工具"},
		{"style": "underline", "quote": " \n "},
	} {
		expectError(t, alice.do("POST", annotationsPath(id, lesson), body), http.StatusBadRequest, "bad_request")
	}
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具"})
	expectError(t, alice.do("PATCH", annotationPath(ann), map[string]any{"style": "highlight", "color": "purple"}), http.StatusBadRequest, "bad_request")
}

func TestOverlappingAnnotationsAreKeptApart(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)
	wide := annotate(t, alice, id, lesson, map[string]any{"style": "highlight", "color": "yellow", "quote": "会思考的程序"})
	inner := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "思考"})

	expectStatus(t, alice.do("DELETE", annotationPath(wide), nil), http.StatusNoContent)
	got := listAnnotations(t, alice, id, lesson)
	if len(got) != 1 || got[0]["id"] != inner["id"] {
		t.Fatalf("unexpected annotations: %v", got)
	}
	if rg := textRange(got[0]); rg == nil || rg[0] != 12 || rg[1] != 14 {
		t.Fatalf("unexpected range: %v", got[0]["range"])
	}
}

func TestAnnotationsAreTheirOwnersAlone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))
	lesson := lessonIDs(t, alice, id)["l00"]
	// alice has not subscribed: she may annotate all the same
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "你好"})

	if got := listAnnotations(t, bob, id, lesson); len(got) != 0 {
		t.Fatalf("bob sees alice's annotations: %v", got)
	}
	expectError(t, bob.do("PATCH", annotationPath(ann), map[string]any{"style": "highlight", "color": "blue"}), http.StatusNotFound, "annotation_not_found")
	expectError(t, bob.do("DELETE", annotationPath(ann), nil), http.StatusNotFound, "annotation_not_found")
	if got := listAnnotations(t, alice, id, lesson); len(got) != 1 || got[0]["style"] != "underline" {
		t.Fatalf("unexpected annotations: %v", got)
	}

	// nobody annotates a course they cannot see
	private := uploadCourse(t, alice, columnFiles(columnManifest()))
	expectError(t, bob.do("POST", annotationsPath(private, lessonIDs(t, alice, private)["l00"]), map[string]any{"style": "underline", "quote": "你好"}),
		http.StatusNotFound, "course_not_found")
}

func TestVideoLessonsTakeNoAnnotations(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)
	// even a video lesson with a text beside it
	lesson := lessonIDs(t, alice, id)["page"]

	expectError(t, alice.do("POST", annotationsPath(id, lesson), map[string]any{"style": "underline", "quote": "文字稿"}), http.StatusConflict, "not_annotatable")
	if got := listAnnotations(t, alice, id, lesson); len(got) != 0 {
		t.Fatalf("expected no annotations, got %v", got)
	}
}

func TestTextIsMatchedAsTheReaderShowsIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	files := columnFiles(columnManifest())
	files["00_开篇词.md"] = "Tom &amp; Jerry 与 <span class=\"x\">内嵌 HTML</span> 以及 \\*星号\\*。\n"
	id := uploadCourse(t, alice, files)
	lesson := lessonIDs(t, alice, id)["l00"]

	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "Tom & Jerry 与 内嵌 HTML 以及 *星号*"})
	if rg := textRange(ann); rg == nil || rg[0] != 0 || rg[1] != 22 {
		t.Fatalf("unexpected range: %v", ann["range"])
	}
}

func TestQuoteMustBeInTheLessonText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	expectError(t, alice.do("POST", annotationsPath(id, lesson), map[string]any{"style": "underline", "quote": "不存在的话"}), http.StatusUnprocessableEntity, "quote_not_found")
	// markup is not text, and neither is the alt text of an image
	expectError(t, alice.do("POST", annotationsPath(id, lesson), map[string]any{"style": "underline", "quote": "**会思考**"}), http.StatusUnprocessableEntity, "quote_not_found")
	expectError(t, alice.do("POST", annotationsPath(id, lesson), map[string]any{"style": "underline", "quote": "示意图"}), http.StatusUnprocessableEntity, "quote_not_found")
	if got := listAnnotations(t, alice, id, lesson); len(got) != 0 {
		t.Fatalf("expected no annotations, got %v", got)
	}
}

func notePath(a map[string]any) string {
	return annotationPath(a) + "/note"
}

func TestNotesAreWrittenEditedAndDeletedWithoutTheAnnotation(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	// a note may be written along with the annotation
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具", "note": "工具调用是 Agent 的手"})
	if ann["note"] != "工具调用是 Agent 的手" {
		t.Fatalf("unexpected note: %v", ann["note"])
	}
	// or added to one that has none
	plain := annotate(t, alice, id, lesson, map[string]any{"style": "highlight", "color": "blue", "quote": "会思考"})
	expectStatus(t, alice.do("PUT", notePath(plain), map[string]any{"note": "思考 = 规划"}), http.StatusNoContent)
	// and edited
	expectStatus(t, alice.do("PUT", notePath(ann), map[string]any{"note": "改过的笔记\n第二行"}), http.StatusNoContent)

	got := listAnnotations(t, alice, id, lesson)
	if len(got) != 2 || got[0]["note"] != "改过的笔记\n第二行" || got[1]["note"] != "思考 = 规划" {
		t.Fatalf("unexpected annotations: %v", got)
	}

	// restyling leaves the note alone
	expectStatus(t, alice.do("PATCH", annotationPath(plain), map[string]any{"style": "underline"}), http.StatusNoContent)
	// deleting the note keeps the annotation
	expectStatus(t, alice.do("DELETE", notePath(ann), nil), http.StatusNoContent)
	got = listAnnotations(t, alice, id, lesson)
	if len(got) != 2 || got[0]["note"] != nil || got[0]["quote"] != "调用工具" || got[1]["note"] != "思考 = 规划" || got[1]["style"] != "underline" {
		t.Fatalf("unexpected annotations: %v", got)
	}
}

func TestBlankNotesAreNotNotes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)

	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具", "note": "  \n "})
	if ann["note"] != nil {
		t.Fatalf("a blank note was kept: %v", ann["note"])
	}
	expectError(t, alice.do("PUT", notePath(ann), map[string]any{"note": " \n"}), http.StatusBadRequest, "bad_request")
	if got := listAnnotations(t, alice, id, lesson); got[0]["note"] != nil {
		t.Fatalf("unexpected annotations: %v", got)
	}
}

func TestNotesAreTheirOwnersAlone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := publishAs(t, a, columnFiles(columnManifest()))
	lesson := lessonIDs(t, alice, id)["l00"]
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "你好", "note": "alice 的笔记"})

	expectError(t, bob.do("PUT", notePath(ann), map[string]any{"note": "bob 写的"}), http.StatusNotFound, "annotation_not_found")
	expectError(t, bob.do("DELETE", notePath(ann), nil), http.StatusNotFound, "annotation_not_found")
	if got := listAnnotations(t, alice, id, lesson); got[0]["note"] != "alice 的笔记" {
		t.Fatalf("unexpected annotations: %v", got)
	}
}
