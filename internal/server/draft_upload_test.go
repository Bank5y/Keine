package server_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/Bank5y/keine/internal/testutil"
)

// newUpload starts a place in the viewer's draft for one lesson's files.
func newUpload(t *testing.T, c *client, id int) string {
	t.Helper()
	r := c.do("POST", draftPath(id)+"/uploads", nil)
	expectStatus(t, r, http.StatusCreated)
	return r.json()["upload"].(string)
}

// putFile sends one file of an upload, named by its path in the upload.
func putFile(c *client, id int, upload, name, body string) resp {
	c.a.t.Helper()
	u := fmt.Sprintf("%s/uploads/%s/files/%s", draftPath(id), upload, (&url.URL{Path: name}).EscapedPath())
	req, _ := http.NewRequest("PUT", c.a.srv.URL+u, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/octet-stream")
	return c.send(req)
}

// uploadFiles starts an upload holding files.
func uploadFiles(t *testing.T, c *client, id int, files map[string]string) string {
	t.Helper()
	u := newUpload(t, c, id)
	for name, body := range files {
		c.mustOK(putFile(c, id, u, name, body))
	}
	return u
}

// draftLesson finds a lesson of the draft's course by title.
func draftLesson(t *testing.T, c *client, id int, title string) map[string]any {
	t.Helper()
	for _, ch := range getDraft(t, c, id)["course"].(map[string]any)["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			if l := l.(map[string]any); l["title"] == title {
				return l
			}
		}
	}
	t.Fatalf("no lesson %q in the draft", title)
	return nil
}

// preview reads a lesson of the draft as the reader would show it.
func preview(c *client, id int, key string) resp {
	return c.do("GET", draftPath(id)+"/lessons/"+key, nil)
}

func TestALessonIsAddedFromItsTextAndImages(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	// files picked one by one lose their folders: img/c.png arrives as c.png
	u := uploadFiles(t, alice, id, map[string]string{
		"03_记忆.md":  "# 03\n\n![](img/c.png)\n\n![](./d.png)\n",
		"c.png":     "c-bytes",
		"d.png":     "d-bytes",
		"stray.txt": "not referenced",
	})

	r := addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜记忆", "content": map[string]any{"upload": u}})

	alice.mustOK(r)
	expectOutline(t, draftOutline(t, alice, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别,03｜记忆")
	l := draftLesson(t, alice, id, "03｜记忆")
	if l["added"] != true || l["hasText"] != true || l["changed"] != true {
		t.Fatalf("the new lesson has its text: %v", l)
	}
	p := alice.mustOK(preview(alice, id, l["key"].(string))).json()
	if p["markdown"] != "# 03\n\n![](img/c.png)\n\n![](./d.png)\n" || p["title"] != "03｜记忆" {
		t.Fatalf("the preview has the lesson's text: %v", p)
	}
	base := p["base"].(string)
	for img, want := range map[string]string{"img/c.png": "c-bytes", "d.png": "d-bytes"} {
		got := alice.mustOK(alice.do("GET", base+img, nil))
		if string(got.body) != want {
			t.Fatalf("the preview's image %s: got %q", img, got.body)
		}
	}
}

func TestACommittedVersionWritesOnlyTheUploadedFiles(t *testing.T) {
	files := &recordingStore{Store: newFileStore(t)}
	a := startApp(t, testutil.NewDatabase(t), files)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	files.written()
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n\n![](img/c.png)\n", "c.png": "c-bytes"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜记忆", "content": map[string]any{"upload": u}}))
	key := draftKeys(t, alice, id)["03｜记忆"]
	uploaded := files.written()

	alice.mustOK(commitDraft(alice, id))

	if w := files.written(); len(w) != 0 {
		t.Fatalf("committing writes nothing anew; uploaded files are copied too: %v", w)
	}
	if len(uploaded) != 2 {
		t.Fatalf("only the uploaded files were ever written: %v", uploaded)
	}
	_, got := filesIn(a, id)
	want := []string{"00_开篇词.md", "01_用户输入.md", "02_意图识别.md", "images/1/a.png", "images/2/b.jpg", u + "/03.md", u + "/img/c.png"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the new version holds the kept files and the new lesson's:\n got  %v\n want %v", got, want)
	}
	text := alice.mustOK(lessonText(alice, id, lessonIDs(t, alice, id)[key])).json()
	if text["markdown"] != "# 03\n\n![](img/c.png)\n" {
		t.Fatalf("the new lesson reads its text: %v", text)
	}
	img := alice.mustOK(alice.do("GET", text["base"].(string)+"img/c.png", nil))
	if string(img.body) != "c-bytes" {
		t.Fatalf("the new lesson's image is served: %q", img.body)
	}
}
