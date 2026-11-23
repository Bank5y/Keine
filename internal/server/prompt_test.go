package server_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestManifestPromptTemplateDownloads(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")

	r := alice.mustOK(alice.do("GET", "/api/manifest/prompt", nil))

	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("unexpected content type %q", r.header.Get("Content-Type"))
	}
	if !strings.Contains(r.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("template should download as a file, got %q", r.header.Get("Content-Disposition"))
	}
	for _, field := range []string{"version", "title", "type", "completion", "intro", "author", "cover", "chapters", "lessons", "file", "video", "url", "duration"} {
		if !strings.Contains(string(r.body), "`"+field+"`") {
			t.Errorf("template should document field %q", field)
		}
	}
}

// The example in the template must stay a manifest the importer accepts.
func TestManifestPromptExampleImports(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	body := string(alice.mustOK(alice.do("GET", "/api/manifest/prompt", nil)).body)

	block := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindStringSubmatch(body)
	if block == nil {
		t.Fatal("template has no ```json example")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(block[1]), &m); err != nil {
		t.Fatalf("example is not JSON: %v", err)
	}
	files := map[string]string{"course.json": block[1]}
	if c, ok := m["cover"].(string); ok {
		files[c] = "cover"
	}
	types := map[string]bool{}
	for _, ch := range m["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			lm := l.(map[string]any)
			if f, ok := lm["file"].(string); ok {
				files[f] = "# " + lm["title"].(string)
			}
			if v, ok := lm["video"].(map[string]any); ok {
				if f, ok := v["file"].(string); ok {
					files[f] = "mp4"
				}
				for k := range v {
					types[k] = true
				}
			}
		}
	}
	if !types["file"] {
		t.Fatal("the example should show a packaged video")
	}

	r := alice.upload("/api/courses", zipOf(t, files))
	expectStatus(t, r, http.StatusCreated)
}

func TestManifestPromptRequiresSignIn(t *testing.T) {
	t.Parallel()
	a := newApp(t)

	expectError(t, a.client().do("GET", "/api/manifest/prompt", nil), http.StatusUnauthorized, "unauthenticated")
}

// The template also shows an ongoing course's outline: an upcoming lesson
// and an empty chapter, which the importer accepts.
func TestManifestPromptShowsAnOutlineThatImports(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	body := string(alice.mustOK(alice.do("GET", "/api/manifest/prompt", nil)).body)
	if !strings.Contains(body, "待更新的讲") {
		t.Fatal("the template explains upcoming lessons")
	}

	var outline string
	for _, b := range regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllStringSubmatch(body, -1) {
		if strings.Contains(b[1], `"ongoing"`) {
			outline = b[1]
		}
	}
	if outline == "" {
		t.Fatal("template has no ```json example of an ongoing course")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(outline), &m); err != nil {
		t.Fatalf("outline example is not JSON: %v", err)
	}
	files := map[string]string{"course.json": outline}
	for _, ch := range m["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			if f, ok := l.(map[string]any)["file"].(string); ok {
				files[f] = "# text"
			}
		}
	}

	id := uploadCourse(t, alice, files)

	d := courseJSON(t, alice, id)
	empty, upcoming := false, false
	for _, ch := range d["chapters"].([]any) {
		lessons := ch.(map[string]any)["lessons"].([]any)
		empty = empty || len(lessons) == 0
		for _, l := range lessons {
			upcoming = upcoming || l.(map[string]any)["upcoming"] == true
		}
	}
	if !empty || !upcoming {
		t.Fatalf("the outline example shows an empty chapter and an upcoming lesson: %v", d["chapters"])
	}
}
