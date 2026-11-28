package server_test

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
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
		if !slices.Contains(c["sources"].([]any), any("concept")) {
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

func TestWithoutAnIndexTheRecallTestFindsPassagesOnly(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	alice := a.user("alice")
	id := uploadCourse(t, alice, linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	got := recallTest(t, admin, id, "意图识别和工具调用", "")

	if hits := recallPathHits(t, got, "concept"); len(hits) != 0 {
		t.Fatalf("no index, no concept hits: %v", hits)
	}
	c := got["candidates"].([]any)
	if len(c) == 0 {
		t.Fatal("the passages find candidates")
	}
	for _, x := range c {
		if fmt.Sprint(x.(map[string]any)["sources"]) != "[passage]" {
			t.Fatalf("a candidate found by its passage: %v", x)
		}
	}
	if f := fused(t, got); !slices.Contains(f, "三层意图 @ 三层意图") || !slices.Contains(f, "工具调用 @ 工具调用") {
		t.Fatalf("the sections sharing words, labelled by their headings: %v", f)
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

func TestTheRecallTestRanksPassagesByBM25(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	id := uploadCourse(t, a.user("alice"), linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))

	got := recallTest(t, admin, id, "意图识别分几层？", "")

	hits := recallPathHits(t, got, "passage")
	if len(hits) == 0 || sectionTitle(hits[0]) != "三层意图" {
		t.Fatalf("the passage sharing most words comes first: %v", hits)
	}
	for i, h := range hits {
		if h["score"].(float64) <= 0 || i > 0 && h["score"].(float64) > hits[i-1]["score"].(float64) {
			t.Fatalf("passages ranked by their scores, best first: %v", hits)
		}
	}
}

func TestASectionBothPathsFindOutranksOneFoundByOnePath(t *testing.T) {
	t.Parallel()
	// Planner is a concept of l00 its text never spells out
	lessons := linkedLessons(concept("Planner", "为什么学 Agent", "规划步骤的组件。"))
	a := aiApp(t, newScriptedModel(lessons...))
	id := publishedIndexedLinked(t, a)

	got := recallTest(t, a.admin(), id, "先说 Planner，再说意图识别。", "l01")

	if c := recallPathHits(t, got, "concept"); len(c) < 2 || c[0]["label"] != "Planner" {
		t.Fatalf("the concept path names Planner first: %v", c)
	}
	c := got["candidates"].([]any)
	first, second := c[0].(map[string]any), c[1].(map[string]any)
	if first["label"] != "意图识别" || fmt.Sprint(first["sources"]) != "[concept passage]" {
		t.Fatalf("found by both paths, 意图识别 comes first: %v", c)
	}
	if second["label"] != "Planner" || fmt.Sprint(second["sources"]) != "[concept]" {
		t.Fatalf("found by the concept path only, Planner comes second: %v", c)
	}
}

func TestANewVersionIsRecalledByItsOwnPassages(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel())
	alice := a.user("alice")
	id := uploadCourse(t, alice, linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	if f := fused(t, recallTest(t, admin, id, "槽位", "")); fmt.Sprint(f) != "[三层意图 @ 三层意图]" {
		t.Fatalf("the first version's passages: %v", f)
	}

	files := linkedFiles()
	files["02_意图识别.md"] = "# 02\n\n## 实体抽取\n\n意图之后抽取实体。\n"
	approveUpdate(t, a, alice, id, files)

	if f := fused(t, recallTest(t, admin, id, "槽位", "")); len(f) != 0 {
		t.Fatalf("the new version's text has no 槽位: %v", f)
	}
	if f := fused(t, recallTest(t, admin, id, "抽取实体", "")); fmt.Sprint(f) != "[实体抽取 @ 实体抽取]" {
		t.Fatalf("the new version's passages: %v", f)
	}
}
