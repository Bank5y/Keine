package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func deleteCourse(c *client, id int) resp {
	return c.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil)
}

func TestSubscriberKeepsDoneLessonsOfADeletedCourse(t *testing.T) {
	a := newApp(t)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	admin := a.admin()

	expectStatus(t, deleteCourse(admin, id), http.StatusNoContent)

	if contains(libraryIDs(t, bob, nil), id) {
		t.Fatal("the deleted course leaves the library")
	}
	expectError(t, alice.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	if contains(myCourseIDs(t, alice), id) {
		t.Fatal("the uploader no longer has the deleted course")
	}
	expectError(t, admin.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")

	d := courseJSON(t, bob, id)
	if d["retention"] != "deleted" || d["retainedCount"] != float64(1) {
		t.Fatalf("bob sees the course as deleted with one retained lesson: %v", d)
	}
	l := bob.mustOK(lessonText(bob, id, lessons["l01"])).json()
	if !strings.Contains(l["markdown"].(string), "# 01") {
		t.Fatalf("the retained lesson is readable: %v", l)
	}
	img := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil))
	if string(img.body) != "png-bytes" {
		t.Fatalf("images of the retained lesson load: %q", img.body)
	}
	expectError(t, lessonText(bob, id, lessons["l02"]), http.StatusNotFound, "lesson_not_found")
	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("the deleted course stays in bob's courses")
	}
	if keys := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(keys) == 0 {
		t.Fatal("the frozen files stay while bob retains lessons")
	}
}

func TestDeletingACourseNobodyRetainsCleansItAtOnce(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := publishedCourse(t, a, alice)
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil)) // subscribed, nothing done
	carol := a.user("carol")
	lessons := lessonIDs(t, carol, id)
	carol.mustOK(setProgress(carol, id, lessons["l01"], map[string]any{"done": true})) // done, never subscribed
	admin := a.admin()

	expectStatus(t, deleteCourse(admin, id), http.StatusNoContent)

	if keys := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(keys) != 0 {
		t.Fatalf("the course files are removed: %v", keys)
	}
	for _, c := range []*client{alice, bob, carol} {
		expectError(t, c.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
		if contains(myCourseIDs(t, c), id) {
			t.Fatal("the course is in nobody's courses")
		}
	}
	expectError(t, deleteCourse(admin, id), http.StatusNotFound, "course_not_found")
}

func TestDeletingAnUnpublishedCourseKeepsItsFreezeAndDropsTheNewerFiles(t *testing.T) {
	a := newApp(t)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	unpublish(t, a, id)
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))
	alice.mustOK(requestPublish(alice, id))
	frozen := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id))

	expectStatus(t, deleteCourse(alice, id), http.StatusNoContent)

	if got := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(frozen) == 0 || len(got) != len(frozen) {
		t.Fatalf("only the frozen folder stays: frozen %v, stored %v", frozen, got)
	}
	d := courseJSON(t, bob, id)
	if d["retention"] != "deleted" || d["title"] != "Agent 系统设计" {
		t.Fatalf("bob reads the course as it was frozen: %v", d)
	}
	bob.mustOK(lessonText(bob, id, lessons["l01"]))
	if contains(myCourseIDs(t, alice), id) {
		t.Fatal("alice no longer has the course")
	}
	admin := a.admin()
	for _, x := range admin.mustOK(admin.do("GET", "/api/admin/reviews", nil)).list("reviews") {
		if int(x.(map[string]any)["course"].(map[string]any)["id"].(float64)) == id {
			t.Fatalf("the deleted course's publish request leaves the queue: %v", x)
		}
	}
}

func TestTheLastRetainerLeavingRemovesADeletedCourse(t *testing.T) {
	a := newApp(t)
	_, bob, id, lessons := subscriberWithDoneLesson(t, a)
	dave := a.user("dave")
	dave.mustOK(dave.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
	dave.mustOK(setProgress(dave, id, lessons["l00"], map[string]any{"done": true}))
	admin := a.admin()
	expectStatus(t, deleteCourse(admin, id), http.StatusNoContent)

	bob.mustOK(bob.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))

	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	if keys := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(keys) == 0 {
		t.Fatal("dave still retains a lesson, so the files stay")
	}
	dave.mustOK(lessonText(dave, id, lessons["l00"]))

	dave.mustOK(dave.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))

	if keys := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(keys) != 0 {
		t.Fatalf("the last retainer leaving removes the course files: %v", keys)
	}
	expectError(t, dave.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	expectError(t, deleteCourse(admin, id), http.StatusNotFound, "course_not_found")
}
