package server_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"
)

func recallPath(id int) string { return fmt.Sprintf("/api/admin/courses/%d/concept-index/recall", id) }

// recallTest is what the admin's recall test finds for a text, leaving out
// the lesson keyed except.
func recallTest(t *testing.T, admin *client, id int, text, except string) map[string]any {
	t.Helper()
	return admin.mustOK(admin.do("POST", recallPath(id), map[string]any{"text": text, "excludeLesson": except})).json()
}

// fused are the recall test's candidates as "label @ section".
func fused(t *testing.T, got map[string]any) []string {
	t.Helper()
	var out []string
	for _, x := range got["candidates"].([]any) {
		c := x.(map[string]any)
		out = append(out, c["label"].(string)+" @ "+sectionTitle(c))
	}
	return out
}

func sectionTitle(c map[string]any) string {
	if s, ok := c["section"].(map[string]any); ok {
		return s["title"].(string)
	}
	return ""
}

// recallPathHits is one recall path's ranking in the recall test.
func recallPathHits(t *testing.T, got map[string]any, source string) []map[string]any {
	t.Helper()
	for _, x := range got["paths"].([]any) {
		p := x.(map[string]any)
		if p["source"] == source {
			var out []map[string]any
			for _, h := range p["hits"].([]any) {
				out = append(out, h.(map[string]any))
			}
			return out
		}
	}
	t.Fatalf("no %s path in %v", source, got["paths"])
	return nil
}

var offeredRe = regexp.MustCompile(`\[s\d+\] (.+)\n位置：讲「[^」]*」(?: › 小节「([^」]*)」)?`)

func TestTheRecallTestFindsWhatTheLinkingStepIsOffered(t *testing.T) {
	t.Parallel()
	const answer = "子节点的回答：目标拆解之后，要先做意图识别，再用 function calling 执行。"
	m := newScriptedModel(linkSteps(linkedLessons(), texts(answer), linkCall())...)
	a := aiApp(t, m)
	bob, courseID, exp := linkedExploration(t, a, true)
	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")
	var offered []string
	for _, g := range offeredRe.FindAllStringSubmatch(lastQuestion(t, contents(m.calls()[5])), -1) {
		offered = append(offered, g[1]+" @ "+g[2])
	}

	got := recallTest(t, a.admin(), courseID, answer, "l01")

	if want := []string{"意图识别 @ 三层意图", "工具调用 @ 工具调用"}; fmt.Sprint(offered) != fmt.Sprint(want) {
		t.Fatalf("the linking step was offered %v", offered)
	}
	if c := fused(t, got); fmt.Sprint(c) != fmt.Sprint(offered) {
		t.Fatalf("the recall test finds %v, the linking step was offered %v", c, offered)
	}
	for _, x := range got["candidates"].([]any) {
		c := x.(map[string]any)
		if fmt.Sprint(c["sources"]) != "[concept]" {
			t.Fatalf("a candidate found by its concept: %v", c)
		}
		if c["score"].(float64) <= 0 {
			t.Fatalf("a fused candidate has a score: %v", c)
		}
	}
}

func TestTheRecallTestRanksEachPathWithItsScores(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel(linkedLessons()...))
	id := publishedIndexedLinked(t, a)
	admin := a.admin()

	got := recallTest(t, admin, id, "先用 Function Calling，再看意图识别和 Intent。", "")

	hits := recallPathHits(t, got, "concept")
	var named []string
	for _, h := range hits {
		named = append(named, fmt.Sprintf("%s %v", h["label"], h["score"]))
	}
	// the concept path ranks by where the text first names a concept, in
	// characters, normalized
	if want := []string{"工具调用 2", "意图识别 20"}; fmt.Sprint(named) != fmt.Sprint(want) {
		t.Fatalf("the concept path ranks %v", named)
	}
	if hits[0]["lesson"].(map[string]any)["key"] != "l00" {
		t.Fatalf("a hit has its lesson: %v", hits[0])
	}
}

func TestTheRecallTestLeavesOutTheLessonExcluded(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkedLessons()...)
	a := aiApp(t, m)
	id := publishedIndexedLinked(t, a)

	got := recallTest(t, a.admin(), id, "意图识别和工具调用", "l02")

	if c := fused(t, got); fmt.Sprint(c) != "[工具调用 @ 工具调用]" {
		t.Fatalf("lesson l02 is left out: %v", c)
	}
}

func TestTheRecallTestCallsNoModelAndRecordsNoUsage(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkedLessons()...)
	a := aiApp(t, m)
	id := publishedIndexedLinked(t, a)
	admin := a.admin()
	before := len(m.calls())
	runs := admin.mustOK(admin.do("GET", "/api/admin/usage", nil)).json()["total"].(map[string]any)["runs"]

	recallTest(t, admin, id, "意图识别和工具调用", "")

	if len(m.calls()) != before {
		t.Fatal("the recall test called the model")
	}
	if now := admin.mustOK(admin.do("GET", "/api/admin/usage", nil)).json()["total"].(map[string]any)["runs"]; now != runs {
		t.Fatalf("runs went from %v to %v", runs, now)
	}
}

func TestOnlyAnAdminCanRunTheRecallTest(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkedLessons()...)
	a := aiApp(t, m)
	id := publishedIndexedLinked(t, a)
	bob := a.user("bob")

	expectStatus(t, bob.do("POST", recallPath(id), map[string]any{"text": "意图识别", "excludeLesson": ""}), http.StatusForbidden)
}

func TestTheRecallTestOfACourseWithoutAnIndexFindsNothing(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	alice := a.user("alice")
	id := uploadCourse(t, alice, linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	got := recallTest(t, admin, id, "意图识别和工具调用", "")

	if c := fused(t, got); len(c) != 0 {
		t.Fatalf("no index, no concept candidates: %v", c)
	}
}

func TestTheRecallTestOfAMissingCourseIsNotFound(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	admin := a.admin()

	expectStatus(t, admin.do("POST", recallPath(999999), map[string]any{"text": "x"}), http.StatusNotFound)
}

// publishedIndexedLinked publishes linkedFiles and builds its index, the
// model replying with linkedLessons.
func publishedIndexedLinked(t *testing.T, a *app) int {
	t.Helper()
	id := uploadCourse(t, a.user("alice"), linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("the index is %q", s)
	}
	return id
}
