package server_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// myAnnotations reads the notes page: every annotation of the viewer's,
// narrowed by the query, and the courses they are on.
func myAnnotations(t *testing.T, c *client, query url.Values) (anns []map[string]any, courses []map[string]any) {
	t.Helper()
	path := "/api/my/annotations"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	r := c.mustOK(c.do("GET", path, nil))
	for _, v := range r.list("annotations") {
		anns = append(anns, v.(map[string]any))
	}
	for _, v := range r.list("courses") {
		courses = append(courses, v.(map[string]any))
	}
	return anns, courses
}

func quotes(anns []map[string]any) []string {
	out := []string{}
	for _, a := range anns {
		out = append(out, a["quote"].(string))
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// courseCounts maps course titles on the notes page to their annotation
// counts.
func courseCounts(courses []map[string]any) map[string]int {
	out := map[string]int{}
	for _, c := range courses {
		out[c["title"].(string)] = int(c["count"].(float64))
	}
	return out
}

// secondColumn uploads another column course, titled differently.
func secondColumn(t *testing.T, c *client) (courseID int, lessons map[string]int) {
	t.Helper()
	m := columnManifest()
	m["title"] = "第二门专栏"
	courseID = uploadCourse(t, c, columnFiles(m))
	return courseID, lessonIDs(t, c, courseID)
}

func TestNotesPageListsEveryAnnotationOfMineWithItsCourseAndLesson(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	first, l00 := annotatedColumn(t, alice)
	second, lessons := secondColumn(t, alice)
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "调用工具"})
	annotate(t, alice, first, l00, map[string]any{"style": "highlight", "color": "green", "quote": "完成任务", "note": "要点"})
	annotate(t, alice, second, lessons["l02"], map[string]any{"style": "highlight", "color": "pink", "quote": "02"})
	// bob's own annotations stay his
	bob := a.user("bob")
	other, bl := annotatedColumn(t, bob)
	annotate(t, bob, other, bl, map[string]any{"style": "underline", "quote": "调用工具"})

	anns, courses := myAnnotations(t, alice, nil)
	if len(anns) != 3 {
		t.Fatalf("alice sees her three annotations: %v", anns)
	}
	if c := courseCounts(courses); len(c) != 2 || c["Agent 系统设计"] != 2 || c["第二门专栏"] != 1 {
		t.Fatalf("unexpected courses: %v", courses)
	}
	var noted map[string]any
	for _, x := range anns {
		if x["quote"] == "完成任务" {
			noted = x
		}
	}
	course, _ := noted["course"].(map[string]any)
	lesson, _ := noted["lesson"].(map[string]any)
	if noted["note"] != "要点" || noted["style"] != "highlight" || noted["color"] != "green" || noted["createdAt"] == nil ||
		course["id"] != float64(first) || course["title"] != "Agent 系统设计" ||
		lesson["id"] != float64(l00) || lesson["title"] != "开篇词" || lesson["chapter"] != "开篇" {
		t.Fatalf("unexpected annotation: %v", noted)
	}
	if rg := textRange(noted); rg == nil || rg[0] != 24 || rg[1] != 28 {
		t.Fatalf("the annotation is located in its lesson: %v", noted["range"])
	}

	if anns, _ := myAnnotations(t, a.user("carol"), nil); len(anns) != 0 {
		t.Fatalf("carol has none: %v", anns)
	}
	expectStatus(t, a.client().do("GET", "/api/my/annotations", nil), http.StatusUnauthorized)
}

func TestNotesPageNarrowsToOneCourse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	first, l00 := annotatedColumn(t, alice)
	second, lessons := secondColumn(t, alice)
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "调用工具"})
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "完成任务"})
	annotate(t, alice, second, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})

	anns, courses := myAnnotations(t, alice, url.Values{"course": {strconv.Itoa(first)}})
	if !sameStrings(quotes(anns), []string{"完成任务", "调用工具"}) {
		t.Fatalf("only the first course's annotations, newest first: %v", quotes(anns))
	}
	// the course list still offers every course, counted as before
	if c := courseCounts(courses); c["Agent 系统设计"] != 2 || c["第二门专栏"] != 1 {
		t.Fatalf("unexpected courses: %v", courses)
	}
	expectStatus(t, alice.do("GET", "/api/my/annotations?course=abc", nil), http.StatusBadRequest)
}

func TestNotesPageFiltersByStyleColourAndNote(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	first, l00 := annotatedColumn(t, alice)
	second, lessons := secondColumn(t, alice)
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "调用工具", "note": "横线笔记"})
	annotate(t, alice, first, l00, map[string]any{"style": "highlight", "color": "yellow", "quote": "完成任务"})
	annotate(t, alice, first, l00, map[string]any{"style": "highlight", "color": "pink", "quote": "会思考", "note": "粉色笔记"})
	annotate(t, alice, second, lessons["l02"], map[string]any{"style": "highlight", "color": "yellow", "quote": "02"})

	cases := []struct {
		query  url.Values
		quotes []string
		counts map[string]int
	}{
		{url.Values{"style": {"underline"}}, []string{"调用工具"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 0}},
		{url.Values{"style": {"highlight"}}, []string{"02", "会思考", "完成任务"}, map[string]int{"Agent 系统设计": 2, "第二门专栏": 1}},
		{url.Values{"color": {"yellow"}}, []string{"02", "完成任务"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 1}},
		{url.Values{"noted": {"1"}}, []string{"会思考", "调用工具"}, map[string]int{"Agent 系统设计": 2, "第二门专栏": 0}},
		{url.Values{"noted": {"1"}, "style": {"highlight"}}, []string{"会思考"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 0}},
		{url.Values{"color": {"yellow"}, "course": {strconv.Itoa(second)}}, []string{"02"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 1}},
	}
	for _, c := range cases {
		anns, courses := myAnnotations(t, alice, c.query)
		if !sameStrings(quotes(anns), c.quotes) {
			t.Errorf("%v: got %v, want %v", c.query, quotes(anns), c.quotes)
		}
		if got := courseCounts(courses); len(got) != len(c.counts) || got["Agent 系统设计"] != c.counts["Agent 系统设计"] || got["第二门专栏"] != c.counts["第二门专栏"] {
			t.Errorf("%v: counts %v, want %v", c.query, got, c.counts)
		}
	}
	for _, bad := range []string{"style=bold", "color=red", "noted=maybe"} {
		expectStatus(t, alice.do("GET", "/api/my/annotations?"+bad, nil), http.StatusBadRequest)
	}
}

func TestNotesPageSearchesQuotesAndNotes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	first, l00 := annotatedColumn(t, alice)
	second, lessons := secondColumn(t, alice)
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "调用工具", "note": "Tool Use 的本质"})
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "tool_call", "prefix": "以及 `", "suffix": "`"})
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "完成任务"})
	annotate(t, alice, second, lessons["l02"], map[string]any{"style": "underline", "quote": "02", "note": "意图要分三层"})

	cases := []struct {
		q      string
		quotes []string
		counts map[string]int
	}{
		{"工具", []string{"调用工具"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 0}},
		// case does not matter, and notes are searched as well as quotes
		{"TOOL", []string{"tool_call", "调用工具"}, map[string]int{"Agent 系统设计": 2, "第二门专栏": 0}},
		{"三层", []string{"02"}, map[string]int{"Agent 系统设计": 0, "第二门专栏": 1}},
		{"  完成任务 ", []string{"完成任务"}, map[string]int{"Agent 系统设计": 1, "第二门专栏": 0}},
		{"没有这个词", []string{}, map[string]int{"Agent 系统设计": 0, "第二门专栏": 0}},
	}
	for _, c := range cases {
		anns, courses := myAnnotations(t, alice, url.Values{"q": {c.q}})
		if !sameStrings(quotes(anns), c.quotes) {
			t.Errorf("%q: got %v, want %v", c.q, quotes(anns), c.quotes)
		}
		if got := courseCounts(courses); got["Agent 系统设计"] != c.counts["Agent 系统设计"] || got["第二门专栏"] != c.counts["第二门专栏"] {
			t.Errorf("%q: counts %v, want %v", c.q, got, c.counts)
		}
	}
}

func TestNotesPageSortsByTimeOrByTheText(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice := a.user("alice")
	first, l00 := annotatedColumn(t, alice)
	second, lessons := secondColumn(t, alice)
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "完成任务"})
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "会思考"})
	annotate(t, alice, second, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})
	annotate(t, alice, second, lessons["l00"], map[string]any{"style": "underline", "quote": "你好"})
	annotate(t, alice, first, l00, map[string]any{"style": "underline", "quote": "Agent", "prefix": "最后一段再说一次 ", "suffix": "。"})

	newest := []string{"Agent", "你好", "02", "会思考", "完成任务"}
	for _, sort := range []string{"", "time"} {
		if anns, _ := myAnnotations(t, alice, url.Values{"sort": {sort}}); !sameStrings(quotes(anns), newest) {
			t.Errorf("sort %q: got %v, want newest first %v", sort, quotes(anns), newest)
		}
	}
	// by the text: course by course (as listed, the latest annotated first),
	// then lesson by lesson, then by place in the lesson
	anns, courses := myAnnotations(t, alice, url.Values{"sort": {"text"}})
	if want := []string{"会思考", "完成任务", "Agent", "你好", "02"}; !sameStrings(quotes(anns), want) {
		t.Errorf("by the text: got %v, want %v", quotes(anns), want)
	}
	if courses[0]["id"] != float64(first) || courses[1]["id"] != float64(second) {
		t.Errorf("courses are listed latest annotated first: %v", courses)
	}
	expectStatus(t, alice.do("GET", "/api/my/annotations?sort=random", nil), http.StatusBadRequest)
}

func TestNotesPageKeepsAnnotationsOnRetainedLessons(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, bob, id, lessons := subscriberWithDoneLesson(t, a)
	annotate(t, bob, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})
	// carol read and annotated without subscribing: she retains nothing
	carol := a.user("carol")
	annotate(t, carol, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})

	unpublish(t, a, id)

	anns, courses := myAnnotations(t, bob, nil)
	if len(anns) != 1 || len(courses) != 1 || courses[0]["count"] != float64(1) {
		t.Fatalf("bob still sees his annotation: %v %v", anns, courses)
	}
	lesson, _ := anns[0]["lesson"].(map[string]any)
	if lesson["id"] != float64(lessons["l02"]) || lesson["title"] != "02｜三层意图识别" || lesson["chapter"] != "意图识别" {
		t.Fatalf("it leads to the retained lesson: %v", anns[0])
	}
	if rg := textRange(anns[0]); rg == nil || rg[0] != 0 || rg[1] != 2 {
		t.Fatalf("located in the retained text: %v", anns[0]["range"])
	}
	if anns, courses := myAnnotations(t, carol, nil); len(anns) != 0 || len(courses) != 0 {
		t.Fatalf("carol can no longer read the course: %v %v", anns, courses)
	}
}

func TestNotesPageKeepsAnnotationsOfADeletedCourse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	annotate(t, bob, id, lessons["l02"], map[string]any{"style": "highlight", "color": "blue", "quote": "02", "note": "留着"})
	annotate(t, alice, id, lessons["l02"], map[string]any{"style": "underline", "quote": "02"})

	expectStatus(t, deleteCourse(a.admin(), id), http.StatusNoContent)

	anns, courses := myAnnotations(t, bob, url.Values{"noted": {"1"}})
	if len(anns) != 1 || len(courses) != 1 || courses[0]["count"] != float64(1) || anns[0]["note"] != "留着" {
		t.Fatalf("bob still sees his annotation on the deleted course: %v %v", anns, courses)
	}
	if lesson, _ := anns[0]["lesson"].(map[string]any); lesson["id"] != float64(lessons["l02"]) {
		t.Fatalf("it leads to the retained lesson: %v", anns[0])
	}
	// the uploader no longer has the course, nor her annotations on it
	if anns, courses := myAnnotations(t, alice, nil); len(anns) != 0 || len(courses) != 0 {
		t.Fatalf("alice lost the deleted course: %v %v", anns, courses)
	}
}
