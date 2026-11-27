package server_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func revisionsPath(id int) string { return conceptIndexPath(id) + "/revisions" }

// revise keeps a revision of the concept extracted as concept: weight 1
// and nothing else changed, unless fields say.
func revise(t *testing.T, admin *client, id int, concept string, fields map[string]any) {
	t.Helper()
	body := map[string]any{"concept": concept, "weight": 1}
	for k, v := range fields {
		body[k] = v
	}
	expectStatus(t, admin.do("PUT", revisionsPath(id)+"/concept", body), http.StatusNoContent)
}

// revisions are the course's revisions as the admin lists them.
func revisions(t *testing.T, admin *client, id int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range admin.mustOK(admin.do("GET", revisionsPath(id), nil)).list("revisions") {
		out = append(out, x.(map[string]any))
	}
	return out
}

// conceptHits are the concept path's labels in the recall test.
func conceptHits(t *testing.T, got map[string]any) []string {
	t.Helper()
	var out []string
	for _, h := range recallPathHits(t, got, "concept") {
		out = append(out, h["label"].(string))
	}
	return out
}

func labels(t *testing.T, got map[string]any) []string {
	t.Helper()
	var out []string
	for _, x := range got["candidates"].([]any) {
		out = append(out, x.(map[string]any)["label"].(string))
	}
	return out
}

// twoConcepts is an index whose l00 has 工具调用 and l02 意图识别, named in
// an answer in that order, each also found by its passage.
func twoConcepts(t *testing.T) (*app, int) {
	t.Helper()
	a := aiApp(t, newScriptedModel(linkedLessons()...))
	return a, publishedIndexedLinked(t, a)
}

const bothNamed = "先说工具调用，再说意图识别。"

func TestAConceptWeightedUpRanksHigher(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()
	if got := labels(t, recallTest(t, admin, id, bothNamed, "l01")); fmt.Sprint(got) != "[工具调用 意图识别]" {
		t.Fatalf("before: %v", got)
	}

	revise(t, admin, id, "意图识别", map[string]any{"weight": 4})

	got := recallTest(t, admin, id, bothNamed, "l01")
	if l := labels(t, got); fmt.Sprint(l) != "[意图识别 工具调用]" {
		t.Fatalf("weighted ×4, 意图识别 comes first: %v", l)
	}
	if effects := got["revisions"].([]any); len(effects) != 1 || effects[0].(map[string]any)["effect"] != "权重 ×4" {
		t.Fatalf("the recall test shows the revision that changed it: %v", effects)
	}
}

func TestADisabledConceptIsNotRecalled(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()

	revise(t, admin, id, "工具调用", map[string]any{"disabled": true})

	got := recallTest(t, admin, id, bothNamed, "l01")
	if c := conceptHits(t, got); fmt.Sprint(c) != "[意图识别]" {
		t.Fatalf("the disabled concept is gone from the concept path: %v", c)
	}
	if e := got["revisions"].([]any); len(e) != 1 || e[0].(map[string]any)["label"] != "工具调用" {
		t.Fatalf("the recall test shows it was disabled: %v", e)
	}
}

func TestAnExcludedLessonOrSectionIsInNeitherPath(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()

	admin.mustOK(admin.do("POST", revisionsPath(id)+"/exclude", map[string]any{"lessonKey": "l00"}))
	admin.mustOK(admin.do("POST", revisionsPath(id)+"/exclude", map[string]any{"lessonKey": "l02", "sectionId": "三层意图"}))

	got := recallTest(t, admin, id, bothNamed+"Agent 是能自己规划步骤的程序。", "l01")
	for _, path := range []string{"concept", "passage"} {
		for _, h := range recallPathHits(t, got, path) {
			if lesson := h["lesson"].(map[string]any)["key"]; lesson == "l00" || lesson == "l02" && sectionTitle(h) == "三层意图" {
				t.Fatalf("the %s path finds what is excluded: %v", path, h)
			}
		}
	}
	if c := got["candidates"].([]any); len(c) != 0 {
		t.Fatalf("nothing is left: %v", c)
	}
	expectError(t, admin.do("POST", revisionsPath(id)+"/exclude", map[string]any{"lessonKey": "l02", "sectionId": "没有这一节"}), http.StatusBadRequest, "section_not_found")
}

func TestAConceptAddedIsRecalledAtItsSection(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()

	admin.mustOK(admin.do("POST", revisionsPath(id)+"/new", map[string]any{
		"name": "槽位填充", "aliases": []string{"slot filling"}, "summary": "把意图需要的参数补齐。", "weight": 1, "lessonKey": "l02", "sectionTitle": "三层意图",
	}))

	got := recallTest(t, admin, id, "用 Slot Filling 补齐参数。", "l01")
	if f := fused(t, got); fmt.Sprint(f) != "[槽位填充 @ 三层意图]" {
		t.Fatalf("the added concept is recalled at its section: %v", f)
	}
	expectError(t, admin.do("POST", revisionsPath(id)+"/new", map[string]any{"name": "x", "weight": 1, "lessonKey": "l02", "sectionTitle": "没有这一节"}), http.StatusBadRequest, "section_not_found")
}

func TestANewAliasIsMatched(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()
	if c := conceptHits(t, recallTest(t, admin, id, "先做 NLU。", "l01")); len(c) != 0 {
		t.Fatalf("before: %v", c)
	}

	revise(t, admin, id, "意图识别", map[string]any{"aliases": []string{"Intent", "NLU"}, "name": "意图理解"})

	if c := conceptHits(t, recallTest(t, admin, id, "先做 NLU。", "l01")); fmt.Sprint(c) != "[意图理解]" {
		t.Fatalf("the new alias is matched, under the new name: %v", c)
	}
}

func TestRevisionsOutliveANewVersionAndGoStaleWithTheirLesson(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(append(linkedLessons(), linkedLessons()...)...)
	a := aiApp(t, m)
	alice := a.user("alice")
	id := uploadCourse(t, alice, linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", id), nil))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)
	revise(t, admin, id, "意图识别", map[string]any{"weight": 4})
	revise(t, admin, id, "工具调用", map[string]any{"weight": 2})
	for _, r := range revisions(t, admin, id) {
		if r["stale"] != false {
			t.Fatalf("a revision just made: %v", r)
		}
	}

	files := linkedFiles()
	files["02_意图识别.md"] = "# 02\n\n## 三层意图\n\n意图识别分三层：领域、意图、槽位，各有各的模型。\n"
	approveUpdate(t, a, alice, id, files)
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	if l := labels(t, recallTest(t, admin, id, bothNamed, "l01")); fmt.Sprint(l) != "[意图识别 工具调用]" {
		t.Fatalf("the revisions still apply to the new version's index: %v", l)
	}
	revs := revisions(t, admin, id)
	stale := map[string]any{}
	for _, r := range revs {
		stale[r["concept"].(string)] = r["stale"]
	}
	if stale["意图识别"] != true || stale["工具调用"] != false {
		t.Fatalf("the revision of the lesson whose text changed may be outdated: %v", revs)
	}
	i := slices.IndexFunc(revs, func(r map[string]any) bool { return r["concept"] == "意图识别" })
	expectStatus(t, admin.do("POST", fmt.Sprintf("%s/%v/confirm", revisionsPath(id), revs[i]["id"]), nil), http.StatusNoContent)
	for _, r := range revisions(t, admin, id) {
		if r["stale"] != false {
			t.Fatalf("confirmed: %v", r)
		}
	}
}

func TestARevisionTakenBackNoLongerApplies(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	admin := a.admin()
	revise(t, admin, id, "工具调用", map[string]any{"disabled": true})

	r := revisions(t, admin, id)[0]
	expectStatus(t, admin.do("DELETE", fmt.Sprintf("%s/%v", revisionsPath(id), r["id"]), nil), http.StatusNoContent)

	if c := conceptHits(t, recallTest(t, admin, id, bothNamed, "l01")); fmt.Sprint(c) != "[工具调用 意图识别]" {
		t.Fatalf("taken back: %v", c)
	}
	expectError(t, admin.do("DELETE", fmt.Sprintf("%s/%v", revisionsPath(id), r["id"]), nil), http.StatusNotFound, "revision_not_found")
}

func TestOnlyAnAdminRevisesTheIndex(t *testing.T) {
	t.Parallel()
	a, id := twoConcepts(t)
	bob := a.user("bob")

	expectStatus(t, bob.do("PUT", revisionsPath(id)+"/concept", map[string]any{"concept": "工具调用", "weight": 4}), http.StatusForbidden)
	expectStatus(t, bob.do("POST", revisionsPath(id)+"/exclude", map[string]any{"lessonKey": "l00"}), http.StatusForbidden)
	expectStatus(t, bob.do("POST", revisionsPath(id)+"/new", map[string]any{"name": "x", "weight": 1, "lessonKey": "l00"}), http.StatusForbidden)
	expectStatus(t, bob.do("GET", revisionsPath(id), nil), http.StatusForbidden)
	expectError(t, a.admin().do("PUT", revisionsPath(id)+"/concept", map[string]any{"concept": "工具调用", "weight": 3}), http.StatusBadRequest, "bad_request")
}
