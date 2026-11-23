package server_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func suggestionsPath(courseID, lessonID int) string {
	return fmt.Sprintf("/api/courses/%d/lessons/%d/suggestions", courseID, lessonID)
}

// suggestions is the magic wand's proposals on a lesson, by annotation id.
func suggestions(t *testing.T, c *client, courseID, lessonID int) map[int]map[string]any {
	t.Helper()
	out := map[int]map[string]any{}
	for _, v := range c.mustOK(c.do("GET", suggestionsPath(courseID, lessonID), nil)).list("suggestions") {
		s := v.(map[string]any)
		out[int(s["annotationId"].(float64))] = s
	}
	return out
}

func annID(ann map[string]any) int { return int(ann["id"].(float64)) }

func expectSuggestion(t *testing.T, s map[string]any, quote string, start, end int) {
	t.Helper()
	if s == nil {
		t.Fatalf("expected a suggestion %q, got none", quote)
	}
	conf, _ := s["confidence"].(float64)
	if rg := textRange(s); s["quote"] != quote || rg == nil || rg[0] != start || rg[1] != end || conf <= 0 || conf > 1 {
		t.Fatalf("expected %q suggested at [%d,%d) with a confidence, got %v", quote, start, end, s)
	}
}

// acceptSuggestion places an annotation where the wand suggests, as the
// migration page does.
func acceptSuggestion(t *testing.T, c *client, ann, s map[string]any) map[string]any {
	t.Helper()
	return c.mustOK(c.do("POST", placePath(ann), map[string]any{"quote": s["quote"], "prefix": s["prefix"], "suffix": s["suffix"]})).json()
}

func TestTheWandSuggestsTheRewordedText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	bob := a.user("bob")
	id, l00, ann := pendingAfterRewording(t, a, bob)

	s := suggestions(t, bob, id, l00)[annID(ann)]
	expectSuggestion(t, s, "使用外部工具", 20, 26)

	// accepting it is the same as placing it on that selection by hand
	expectAttached(t, acceptSuggestion(t, bob, ann, s), 20, 26)
	g := annotationByID(t, listAnnotations(t, bob, id, l00), ann)
	expectAttached(t, g, 20, 26)
	if g["quote"] != "使用外部工具" || g["note"] != "关键能力" {
		t.Fatalf("expected the annotation moved with its note, got %v", g)
	}
	if left := suggestions(t, bob, id, l00); len(left) != 0 {
		t.Fatalf("nothing awaits migration, so nothing is suggested: %v", left)
	}
}

func TestTheWandSuggestsPlacesForTypicalEditsAndNoneForDeletedText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id, lessons := publishedAnnotatedColumn(t, a, alice)
	l00 := lessons["l00"]
	word := annotate(t, bob, id, l00, map[string]any{"style": "underline", "quote": "会思考的程序", "prefix": "Agent 是一个", "suffix": "，它会调用工具"})
	punct := annotate(t, bob, id, l00, map[string]any{"style": "underline", "quote": "它会调用工具完成任务", "prefix": "会思考的程序，", "suffix": "。第二段"})
	gone := annotate(t, bob, id, l00, map[string]any{"style": "highlight", "color": "blue",
		"quote": "以及 tool_call 这个字段", "prefix": "第二段也提到 Agent，", "suffix": "。func main"})
	kept := annotate(t, bob, id, l00, map[string]any{"style": "underline", "quote": "最后一段"})

	// one character changes, the punctuation around the tool sentence
	// changes, and the second paragraph is deleted
	revised := strings.Replace(openingText, "**会思考**的程序，它会[调用工具](https://example.com)完成任务。",
		"**会思索**的程序；它会调用工具，完成任务。", 1)
	revised = strings.Replace(revised, "第二段也提到 Agent，以及 `tool_call` 这个字段。\n\n", "", 1)
	a.admin().mustOK(updateCourse(t, a.admin(), id, annotatedFiles(revised)))

	got := suggestions(t, bob, id, l00)
	expectSuggestion(t, got[annID(word)], "会思索的程序", 11, 17)
	expectSuggestion(t, got[annID(punct)], "它会调用工具，完成任务", 18, 29)
	if s, ok := got[annID(gone)]; ok {
		t.Fatalf("deleted text gets no suggestion, got %v", s)
	}
	if s, ok := got[annID(kept)]; ok {
		t.Fatalf("an attached annotation gets no suggestion, got %v", s)
	}

	// the same input gives the same suggestions every time
	first, _ := json.Marshal(got)
	for range 3 {
		if again, _ := json.Marshal(suggestions(t, bob, id, l00)); string(again) != string(first) {
			t.Fatalf("suggestions differ: %s vs %s", again, first)
		}
	}

	// accepting them all leaves only the deleted text to handle by hand
	for _, ann := range []map[string]any{word, punct} {
		acceptSuggestion(t, bob, ann, got[annID(ann)])
	}
	list := listAnnotations(t, bob, id, l00)
	expectAttached(t, annotationByID(t, list, word), 11, 17)
	expectAttached(t, annotationByID(t, list, punct), 18, 29)
	if g := annotationByID(t, list, gone); g["state"] != "pending" {
		t.Fatalf("the deleted text's annotation still awaits migration, got %v", g)
	}

	// suggestions are the reader's own
	if other := suggestions(t, a.user("carol"), id, l00); len(other) != 0 {
		t.Fatalf("carol sees none of bob's suggestions: %v", other)
	}
}
