package server_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestManifestPromptTemplateDownloads(t *testing.T) {
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
	a := newApp(t)

	expectError(t, a.client().do("GET", "/api/manifest/prompt", nil), http.StatusUnauthorized, "unauthenticated")
}
