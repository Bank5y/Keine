package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func unpublish(t *testing.T, a *app, id int) {
	t.Helper()
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/unpublish", id), nil))
}

func lessonText(c *client, courseID, lessonID int) resp {
	return c.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", courseID, lessonID), nil)
}

// lessonsByKey maps lesson keys to their entries in the course detail.
func lessonsByKey(d map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, ch := range d["chapters"].([]any) {
		for _, l := range ch.(map[string]any)["lessons"].([]any) {
			m := l.(map[string]any)
			out[m["key"].(string)] = m
		}
	}
	return out
}

// subscriberWithDoneLesson publishes alice's column course and has bob
// subscribe and finish l01.
func subscriberWithDoneLesson(t *testing.T, a *app) (alice, bob *client, id int, lessons map[string]int) {
	t.Helper()
	alice = a.user("alice")
	bob = a.user("bob")
	id = publishedCourse(t, a, alice)
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
	lessons = lessonIDs(t, bob, id)
	bob.mustOK(setProgress(bob, id, lessons["l01"], map[string]any{"done": true}))
	return alice, bob, id, lessons
}

func TestSubscriberKeepsDoneLessonsOfAnUnpublishedCourse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, bob, id, lessons := subscriberWithDoneLesson(t, a)
	carol := a.user("carol")
	carol.mustOK(setProgress(carol, id, lessons["l02"], map[string]any{"done": true})) // read, never subscribed

	unpublish(t, a, id)

	d := courseJSON(t, bob, id)
	if d["retention"] != "unpublished" || d["retainedCount"] != float64(1) {
		t.Fatalf("bob should see the course as unpublished with one retained lesson: %v", d)
	}
	if p := d["progress"].(map[string]any); p["done"] != float64(1) || p["total"] != float64(3) {
		t.Fatalf("progress still counts every lesson: %v", p)
	}
	byKey := lessonsByKey(d)
	if byKey["l01"]["unavailable"] == true || byKey["l00"]["unavailable"] != true || byKey["l02"]["unavailable"] != true {
		t.Fatalf("only the done lesson stays readable: %v", byKey)
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
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d/files/02_意图识别.md", id), nil), http.StatusNotFound, "file_not_found")
	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("the unpublished course stays in bob's courses")
	}

	expectError(t, carol.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	expectError(t, lessonText(carol, id, lessons["l02"]), http.StatusNotFound, "course_not_found")
}

func TestRepublishingShowsSubscribersTheCurrentCourseAndCleansTheFreeze(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice, bob, id, _ := subscriberWithDoneLesson(t, a)
	unpublish(t, a, id)
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))

	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	d := courseJSON(t, bob, id)
	if d["retention"] != nil || d["title"] != "Agent 系统设计 (第二版)" || d["subscribed"] != true {
		t.Fatalf("bob is still subscribed and sees the current course: %v", d)
	}
	byKey := lessonsByKey(d)
	if byKey["l03"] == nil || byKey["l02"]["unavailable"] != nil {
		t.Fatalf("every current lesson is readable again: %v", byKey)
	}
	if keys := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id)); len(keys) != 0 {
		t.Fatalf("the frozen folder is cleaned once nobody needs it: %v", keys)
	}
	if !contains(myCourseIDs(t, bob), id) {
		t.Fatal("the course is in bob's courses")
	}
}

func TestRepublishingKeepsFilesTheCourseStillUses(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, bob, id, lessons := subscriberWithDoneLesson(t, a)
	unpublish(t, a, id)
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	if keys := a.storedKeys(fmt.Sprintf("courses/%d/", id)); len(keys) == 0 {
		t.Fatal("the folder the course still uses stays")
	}
	bob.mustOK(lessonText(bob, id, lessons["l02"]))
}

func TestUnsubscribingGivesUpRetainedLessons(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	dave := a.user("dave")
	dave.mustOK(dave.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))
	dave.mustOK(setProgress(dave, id, lessons["l00"], map[string]any{"done": true}))
	unpublish(t, a, id)
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))

	bob.mustOK(bob.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))

	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
	expectError(t, lessonText(bob, id, lessons["l01"]), http.StatusNotFound, "course_not_found")
	expectError(t, bob.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil), http.StatusNotFound, "course_not_found")
	if contains(myCourseIDs(t, bob), id) {
		t.Fatal("the course leaves bob's courses")
	}
	if keys := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id)); len(keys) == 0 {
		t.Fatal("dave still retains a lesson, so the freeze stays")
	}
	dave.mustOK(lessonText(dave, id, lessons["l00"]))

	dave.mustOK(dave.do("DELETE", fmt.Sprintf("/api/courses/%d/subscribe", id), nil))

	if keys := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id)); len(keys) != 0 {
		t.Fatalf("the last retainer leaving cleans the frozen folder: %v", keys)
	}
	if mine := courseJSON(t, alice, id); mine["title"] != "Agent 系统设计 (第二版)" {
		t.Fatalf("alice's course is untouched: %v", mine)
	}
}

func TestUnpublishingWithoutRetainersFreezesNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	bob := a.user("bob")
	id := publishedCourse(t, a, alice)
	bob.mustOK(bob.do("POST", fmt.Sprintf("/api/courses/%d/subscribe", id), nil)) // subscribed, nothing done
	unpublish(t, a, id)
	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))

	if keys := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id)); len(keys) != 0 {
		t.Fatalf("with nothing retained the old folder goes as usual: %v", keys)
	}
	expectError(t, bob.do("GET", fmt.Sprintf("/api/courses/%d", id), nil), http.StatusNotFound, "course_not_found")
}

func TestRetainedLessonsKeepTheirContentWhenTheUploaderUpdates(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	unpublish(t, a, id)
	frozen := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id))

	_, files := nextVersion()
	alice.mustOK(updateCourse(t, alice, id, files))

	l := bob.mustOK(lessonText(bob, id, lessons["l01"])).json()
	if l["title"] != "01｜用户输入规范化" || strings.Contains(l["markdown"].(string), "修订版") {
		t.Fatalf("bob keeps the lesson as it was when the course was unpublished: %v", l)
	}
	d := courseJSON(t, bob, id)
	if d["title"] != "Agent 系统设计" || d["lessonCount"] != float64(3) || lessonsByKey(d)["l03"] != nil {
		t.Fatalf("bob keeps the contents as they were: %v", d)
	}
	img := bob.mustOK(bob.do("GET", fmt.Sprintf("/api/courses/%d/files/images/1/a.png", id), nil))
	if string(img.body) != "png-bytes" {
		t.Fatalf("frozen images still load: %q", img.body)
	}
	if got := a.storedKeys(fmt.Sprintf("courses/%d/v1/", id)); len(frozen) == 0 || len(got) != len(frozen) {
		t.Fatalf("the frozen folder is kept: before %v, after %v", frozen, got)
	}
	if mine := courseJSON(t, alice, id); mine["title"] != "Agent 系统设计 (第二版)" || mine["retention"] != nil {
		t.Fatalf("alice sees her updated course: %v", mine)
	}
}

func TestSubscriberKeepsAnnotatedLessonsOfAnUnpublishedCourse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, bob, id, lessons := subscriberWithDoneLesson(t, a)
	ann := annotate(t, bob, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"}) // annotated, not done

	unpublish(t, a, id)

	d := courseJSON(t, bob, id)
	if d["retainedCount"] != float64(2) {
		t.Fatalf("bob retains the done and the annotated lesson: %v", d)
	}
	byKey := lessonsByKey(d)
	if byKey["l02"]["unavailable"] == true || byKey["l02"]["state"] != "unstarted" || byKey["l00"]["unavailable"] != true {
		t.Fatalf("the annotated lesson stays readable and unfinished: %v", byKey)
	}
	l := bob.mustOK(lessonText(bob, id, lessons["l02"])).json()
	if !strings.Contains(l["markdown"].(string), "# 02") {
		t.Fatalf("the annotated lesson is readable: %v", l)
	}
	got := listAnnotations(t, bob, id, lessons["l02"])
	if len(got) != 1 || got[0]["id"] != ann["id"] {
		t.Fatalf("bob's annotation is still there: %v", got)
	}
	if rg := textRange(got[0]); rg == nil || rg[0] != 0 || rg[1] != 2 {
		t.Fatalf("the annotation sits where it was: %v", got[0]["range"])
	}
}
