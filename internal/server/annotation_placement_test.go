package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// pendingAfterRewording annotates the tool sentence of annotatedColumn's
// opening lesson for bob, with a note, then has the admin reword that
// sentence so the annotation awaits migration.
func pendingAfterRewording(t *testing.T, a *app, bob *client) (courseID, lessonID int, ann map[string]any) {
	t.Helper()
	alice := a.user("alice")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	ann = annotate(t, bob, id, lessons["l00"], map[string]any{"style": "highlight", "color": "green",
		"quote": "调用工具", "prefix": "它会", "suffix": "完成任务", "note": "关键能力"})
	revised := strings.Replace(openingText, "[调用工具](https://example.com)", "使用外部工具", 1)
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(revised)))
	if g := annotationByID(t, listAnnotations(t, bob, id, lessons["l00"]), ann); g["state"] != "pending" {
		t.Fatalf("expected the annotation pending, got %v", g)
	}
	return id, lessons["l00"], ann
}

func placePath(ann map[string]any) string { return annotationPath(ann) + "/place" }
func excerptPath(ann map[string]any) string { return annotationPath(ann) + "/excerpt" }

func TestPendingAnnotationIsPlacedOnASelectionOfTheNewText(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)

	r := bob.do("POST", placePath(ann), map[string]any{"quote": "使用外部工具", "prefix": "它会", "suffix": "完成任务"})
	expectStatus(t, r, http.StatusOK)
	expectAttached(t, r.json(), 20, 26)

	got := listAnnotations(t, bob, id, l00)
	g := annotationByID(t, got, ann)
	expectAttached(t, g, 20, 26)
	if g["quote"] != "使用外部工具" || g["note"] != "关键能力" || g["style"] != "highlight" || g["color"] != "green" {
		t.Fatalf("expected the annotation moved with its note and look, got %v", g)
	}
	for _, x := range got {
		if x["state"] == "pending" {
			t.Fatalf("nothing should await migration any more: %v", got)
		}
	}
}

func TestPlacingNeedsTextThatIsInTheLesson(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)

	expectError(t, bob.do("POST", placePath(ann), map[string]any{"quote": "调用工具"}), http.StatusUnprocessableEntity, "quote_not_found")
	expectError(t, a.user("carol").do("POST", placePath(ann), map[string]any{"quote": "使用外部工具"}), http.StatusNotFound, "annotation_not_found")
	if g := annotationByID(t, listAnnotations(t, bob, id, l00), ann); g["state"] != "pending" {
		t.Fatalf("a failed placement leaves the annotation pending, got %v", g)
	}
}

func TestPendingAnnotationIsKeptAsAnExcerpt(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)

	expectStatus(t, bob.do("POST", excerptPath(ann), nil), http.StatusNoContent)

	g := annotationByID(t, listAnnotations(t, bob, id, l00), ann)
	if g["state"] != "changed" || g["range"] != nil || g["note"] != "关键能力" {
		t.Fatalf("expected an excerpt whose text changed, keeping its note, got %v", g)
	}
	anns, _ := myAnnotations(t, bob, url.Values{})
	if len(anns) != 1 || anns[0]["state"] != "changed" || anns[0]["quote"] != "调用工具" {
		t.Fatalf("the notes page shows it as changed: %v", anns)
	}
	// only an annotation awaiting migration is kept as an excerpt
	expectError(t, bob.do("POST", excerptPath(ann), nil), http.StatusConflict, "annotation_not_pending")
	expectError(t, a.user("carol").do("POST", excerptPath(ann), nil), http.StatusNotFound, "annotation_not_found")
}

func TestAnExcerptMayStillBePlaced(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)
	expectStatus(t, bob.do("POST", excerptPath(ann), nil), http.StatusNoContent)

	bob.mustOK(bob.do("POST", placePath(ann), map[string]any{"quote": "外部工具"}))
	expectAttached(t, annotationByID(t, listAnnotations(t, bob, id, l00), ann), 22, 26)
}

func TestPendingAnnotationIsDeleted(t *testing.T) {
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)

	expectStatus(t, bob.do("DELETE", annotationPath(ann), nil), http.StatusNoContent)
	if got := listAnnotations(t, bob, id, l00); len(got) != 0 {
		t.Fatalf("expected the annotation gone, got %v", got)
	}
}

func TestAnAttachedAnnotationIsNotPlacedAgain(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id, lesson := annotatedColumn(t, alice)
	ann := annotate(t, alice, id, lesson, map[string]any{"style": "underline", "quote": "调用工具"})
	expectError(t, alice.do("POST", placePath(ann), map[string]any{"quote": "会思考"}), http.StatusConflict, "annotation_not_pending")
}
