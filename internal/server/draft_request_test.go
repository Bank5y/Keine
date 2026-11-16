package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func approveUpdateOf(t *testing.T, admin *client, courseID int) {
	t.Helper()
	reviews := updateReviews(t, admin, courseID)
	if len(reviews) != 1 {
		t.Fatalf("expected one update review, got %v", reviews)
	}
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", int(reviews[0]["id"].(float64))), nil))
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
