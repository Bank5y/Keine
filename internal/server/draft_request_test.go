package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// changeLines renders a review's changes chapter by chapter, as
// "chapter[its changes]: lesson[its changes], ...". A lesson moved from
// another chapter shows "<-that chapter" and an upcoming one "(upcoming)".
func changeLines(t *testing.T, review map[string]any) []string {
	t.Helper()
	tagged := func(x map[string]any) string {
		s := x["title"].(string)
		if cs := x["changes"].([]any); len(cs) > 0 {
			var names []string
			for _, c := range cs {
				names = append(names, c.(string))
			}
			s += "[" + strings.Join(names, ",") + "]"
		}
		if from, _ := x["fromChapter"].(string); from != "" {
			s += " <-" + from
		}
		if x["upcoming"] == true {
			s += " (upcoming)"
		}
		return s
	}
	var out []string
	for _, cx := range review["changes"].(map[string]any)["chapters"].([]any) {
		ch := cx.(map[string]any)
		var lessons []string
		for _, lx := range ch["lessons"].([]any) {
			lessons = append(lessons, tagged(lx.(map[string]any)))
		}
		out = append(out, tagged(ch)+": "+strings.Join(lessons, ", "))
	}
	return out
}

func expectChanges(t *testing.T, review map[string]any, want ...string) {
	t.Helper()
	if got := changeLines(t, review); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected changes:\n got  %q\n want %q", got, want)
	}
}

func approveUpdateOf(t *testing.T, admin *client, courseID int) {
	t.Helper()
	reviews := updateReviews(t, admin, courseID)
	if len(reviews) != 1 {
		t.Fatalf("expected one update review, got %v", reviews)
	}
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(reviews[0]["id"].(float64))), nil))
}

func TestTheReviewListsRenamedAndMovedLessonsByChapter(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	subscribe(t, carol, id)
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameChapter", "chapter": "opening", "title": "序言"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "moveLesson", "lesson": "l00", "chapter": "intent", "after": "l02"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "moveLesson", "lesson": "l02", "chapter": "intent", "after": ""}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))
	alice.mustOK(commitDraft(alice, id))

	r := updateReviews(t, admin, id)[0]

	if c := r["changes"].(map[string]any); c["renamed"] != float64(1) || c["moved"] != float64(2) || c["modified"] != float64(0) || c["kept"] != float64(3) {
		t.Fatalf("one lesson renamed, two moved, none modified: %v", c)
	}
	expectChanges(t, r,
		"序言[renamed]: ",
		"意图识别: 02｜三层意图识别[moved], 01｜输入[renamed], 开篇词[moved] <-开篇")
	ch := r["changes"].(map[string]any)["chapters"].([]any)
	if prev := ch[0].(map[string]any)["previousTitle"]; prev != "开篇" {
		t.Fatalf("a renamed chapter shows its old title: %v", ch[0])
	}
	if prev := ch[1].(map[string]any)["lessons"].([]any)[1].(map[string]any)["previousTitle"]; prev != "01｜用户输入规范化" {
		t.Fatalf("a renamed lesson shows its old title: %v", ch[1])
	}

	approveUpdateOf(t, admin, id)

	if p := onlyNotification(t, carol, "course_updated"); p["renamed"] != float64(1) || p["moved"] != float64(2) {
		t.Fatalf("subscribers are told of the renamed and moved lessons: %v", p)
	}
}

func TestSubmittingAgainReplacesTheRequestersPendingRequest(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	carol := a.user("carol")
	admin := a.admin()
	id := publishedWith(t, a, alice, func(m map[string]any) { m["completion"] = "ongoing" })
	_, files := nextVersion()
	bob.mustOK(submitUpdate(t, bob, id, files))
	carol.mustOK(addOp(carol, id, map[string]any{"type": "renameLesson", "lesson": "l02", "title": "02｜意图"}))
	carol.mustOK(commitDraft(carol, id))
	bob.mustOK(addOp(bob, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))

	bob.mustOK(commitDraft(bob, id))

	reqs := myUpdateRequests(t, bob, id)
	if len(reqs) != 2 || reqs[0].(map[string]any)["state"] != "pending" || reqs[1].(map[string]any)["state"] != "withdrawn" {
		t.Fatalf("the new request replaces the old one: %v", reqs)
	}
	if n := len(updateReviews(t, admin, id)); n != 2 {
		t.Fatalf("admins review bob's latest request and carol's, got %d", n)
	}
	for _, k := range a.storedKeys(fmt.Sprintf("courses/%d/", id)) {
		if strings.HasSuffix(k, "03_plan.md") {
			t.Fatalf("the replaced request's files are removed, found %s", k)
		}
	}
	unread := 0
	for _, n := range notificationsOfKind(t, admin, "update_requested") {
		if n["read"] == false {
			unread++
		}
	}
	if unread != 2 {
		t.Fatalf("only the requests still pending call for the admin, got %d unread", unread)
	}

	// a package replaces a pending draft request the same way
	bob.mustOK(submitUpdate(t, bob, id, files))

	if reqs := myUpdateRequests(t, bob, id); len(reqs) != 3 || reqs[1].(map[string]any)["state"] != "withdrawn" {
		t.Fatalf("a package replaces the request too: %v", reqs)
	}
}

func TestAPublicCoursesUploaderCommitsADraftAsAnUpdateRequest(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	bob.mustOK(setProgress(bob, id, lessonIDs(t, bob, id)["l01"], map[string]any{"done": true}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "deleteLesson", "lesson": "l00"}))
	alice.mustOK(addOp(alice, id, map[string]any{"type": "renameLesson", "lesson": "l01", "title": "01｜输入"}))

	r := alice.mustOK(commitDraft(alice, id))

	if r.json()["outcome"] != "requested" {
		t.Fatalf("a public course's uploader asks for the update: %s", r.body)
	}
	expectOutline(t, courseOutline(t, bob, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别")
	if n := len(draftOps(t, alice, id)); n != 0 {
		t.Fatalf("the draft became the request, got %d operations left", n)
	}
	if reqs := myUpdateRequests(t, alice, id); len(reqs) != 1 || reqs[0].(map[string]any)["state"] != "pending" {
		t.Fatalf("the requester sees a pending request: %v", reqs)
	}
	onlyNotification(t, admin, "update_requested")

	approveUpdateOf(t, admin, id)

	expectOutline(t, courseOutline(t, bob, id), "开篇:", "意图识别:01｜输入,02｜三层意图识别")
	if s := lessonStates(t, bob, id)["l01"]; s != "done" {
		t.Fatalf("learners keep their records, got %s", s)
	}
	l := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, lessonIDs(t, bob, id)["l01"]), nil)).json()
	if !strings.Contains(l["markdown"].(string), "images/1/a.png") {
		t.Fatalf("the lesson keeps its text: %v", l)
	}
	expectStatus(t, bob.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil), http.StatusOK)
}

func TestARequestedDraftCarriesItsUploadedLessons(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	admin := a.admin()
	id := publishedCourse(t, a, alice)
	u := uploadFiles(t, alice, id, map[string]string{"03.md": "# 03\n\n![](c.png)\n", "c.png": "c-bytes"})
	alice.mustOK(addOp(alice, id, map[string]any{"type": "addLesson", "chapter": "intent", "title": "03｜记忆", "content": map[string]any{"upload": u}}))
	key := draftKeys(t, alice, id)["03｜记忆"]

	if r := alice.mustOK(commitDraft(alice, id)).json(); r["outcome"] != "requested" {
		t.Fatalf("the draft becomes an update request: %v", r)
	}
	if left := draftArea(a, id); len(left) != 0 {
		t.Fatalf("the requested draft's area is removed: %v", left)
	}

	approveUpdateOf(t, admin, id)

	expectOutline(t, courseOutline(t, bob, id), "开篇:开篇词", "意图识别:01｜用户输入规范化,02｜三层意图识别,03｜记忆")
	lesson := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, lessonIDs(t, bob, id)[key]), nil)).json()
	if lesson["markdown"] != "# 03\n\n![](c.png)\n" {
		t.Fatalf("the approved course reads the uploaded text: %v", lesson["markdown"])
	}
	if r := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/files/%s/c.png", id, u), nil)); string(r.body) != "c-bytes" {
		t.Fatalf("the approved course serves the uploaded image: %q", r.body)
	}
}
