package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/testutil"
)

func commitDraft(c *client, id int) resp {
	return c.do("POST", draftPath(id)+"/commit", nil)
}

// recordingStore is a file store that remembers the keys written to it.
type recordingStore struct {
	storage.Store
	mu   sync.Mutex
	puts []string
}

func (s *recordingStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	s.mu.Lock()
	s.puts = append(s.puts, key)
	s.mu.Unlock()
	return s.Store.Put(ctx, key, r, size)
}

// written returns the keys written since the last call.
func (s *recordingStore) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.puts
	s.puts = nil
	return out
}

// filesIn lists the files of the folder a course reads, relative to it.
func filesIn(a *app, courseID int) (dir string, files []string) {
	a.t.Helper()
	prefix := fmt.Sprintf("courses/%d/", courseID)
	dirs := map[string]bool{}
	for _, k := range a.storedKeys(prefix) {
		d, f, _ := strings.Cut(strings.TrimPrefix(k, prefix), "/")
		dirs[d] = true
		files = append(files, d+"/"+f)
	}
	if len(dirs) != 1 {
		a.t.Fatalf("the course should keep exactly one folder of files, found %v", dirs)
	}
	for d := range dirs {
		dir = d
	}
	for i := range files {
		files[i] = strings.TrimPrefix(files[i], dir+"/")
	}
	slices.Sort(files)
	return dir, files
}

func TestCommittingADraftUpdatesThePrivateCourseKeepingProgress(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	m := columnManifest()
	m["completion"] = "ongoing"
	id := uploadCourse(t, alice, columnFiles(m))
	before := lessonIDs(t, alice, id)
	alice.mustOK(setProgress(alice, id, before["l02"], map[string]any{"done": true}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addChapter", "title": "计划"}))
	plan := draftKeys(t, alice, id)["计划"]
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addUpcomingLesson", "chapter": plan, "title": "03｜Plan"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "moveLesson", "lesson": "l02", "chapter": plan, "after": ""}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l02", "title": "02｜意图"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteChapter", "chapter": "opening"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "setInfo", "info": map[string]any{"title": "新课名", "authorName": "小明"}}))
	want := draftOutline(t, alice, id)

	r := alice.mustOK(commitDraft(alice, id))

	if r.json()["outcome"] != "updated" {
		t.Fatalf("a private course's uploader updates it directly: %s", r.body)
	}
	expectOutline(t, courseOutline(t, alice, id), want...)
	expectOutline(t, want, "意图识别:01｜用户输入规范化", "计划:02｜意图,03｜Plan")
	d := courseJSON(t, alice, id)
	if d["title"] != "新课名" || d["authorName"] != "小明" || d["intro"] != "一门讲 Agent 的专栏。" {
		t.Fatalf("the course information follows the draft: %v", d)
	}
	after := lessonIDs(t, alice, id)
	if after["l02"] != before["l02"] || after["l01"] != before["l01"] {
		t.Fatal("kept lessons keep their ids")
	}
	if s := lessonStates(t, alice, id)["l02"]; s != "done" {
		t.Fatalf("a moved and renamed lesson keeps its progress, got %s", s)
	}
	if p := courseProgress(t, alice, id); p["done"] != float64(1) || p["total"] != float64(2) {
		t.Fatalf("progress counts the lessons with content: %v", p)
	}
	l := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, after["l02"]), nil)).json()
	if !strings.Contains(l["markdown"].(string), "images/2/b.jpg") {
		t.Fatalf("the lesson keeps its text: %v", l)
	}
	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("a committed draft is gone, got %d operations", n)
	}
}

func TestACommittedVersionCopiesTheFilesItKeeps(t *testing.T) {
	files := &recordingStore{Store: newFileStore(t)}
	a := startApp(t, testutil.NewDatabase(t), files)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	oldDir, _ := filesIn(a, id)
	files.written()
	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteLesson", "lesson": "l02"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))

	alice.mustOK(commitDraft(alice, id))

	dir, got := filesIn(a, id)
	if dir == oldDir {
		t.Fatal("the update is a new version in its own folder")
	}
	if want := []string{"00_开篇词.md", "01_用户输入.md", "images/1/a.png"}; !slices.Equal(got, want) {
		t.Fatalf("the new version holds the files its lessons use:\n got  %v\n want %v", got, want)
	}
	if w := files.written(); len(w) != 0 {
		t.Fatalf("unchanged files are copied, not written: %v", w)
	}
	expectStatus(t, alice.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil), http.StatusOK)
}

func TestAnAdminCommitsADraftOfAPublicCourseAndSubscribersAreTold(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	subscribe(t, carol, id)
	admin.mustOK(addOp(admin, id, map[string]any{"type": "deleteLesson", "lesson": "l00"}))

	r := admin.mustOK(commitDraft(admin, id))

	if r.json()["outcome"] != "updated" {
		t.Fatalf("an admin updates a public course directly: %s", r.body)
	}
	expectOutline(t, courseOutline(t, carol, id), "开篇:", "意图识别:01｜用户输入规范化,02｜三层意图识别")
	if p := onlyNotification(t, carol, "course_updated"); p["removed"] != float64(1) || p["added"] != float64(0) {
		t.Fatalf("subscribers are told what changed: %v", p)
	}
}

func TestAnEmptyDraftCannotBeCommitted(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))

	expectError(t, commitDraft(alice, id), http.StatusConflict, "draft_empty")
}
