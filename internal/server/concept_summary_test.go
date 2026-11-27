package server_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
)

// as gives a concept of an extraction the role its lesson has for it.
func as(role string, c map[string]any) map[string]any {
	c["role"] = role
	return c
}

// lessonOf is a model's v2 reply for one lesson: its summary and concepts.
func lessonOf(summary string, entries ...map[string]any) step {
	if entries == nil {
		entries = []map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{"summary": summary, "concepts": entries})
	return texts(string(b))
}

// indexConcepts are the merged concepts of the course's current index.
func indexConcepts(t *testing.T, admin *client, id int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range admin.mustOK(admin.do("GET", conceptIndexPath(id)+"/concepts", nil)).list("concepts") {
		out = append(out, x.(map[string]any))
	}
	return out
}

func conceptNamed(t *testing.T, concepts []map[string]any, name string) map[string]any {
	t.Helper()
	for _, c := range concepts {
		if c["name"] == name {
			return c
		}
	}
	t.Fatalf("no concept %q in %v", name, concepts)
	return nil
}

// builtWith publishes indexedFiles and builds its index, the model
// replying with steps.
func builtWith(t *testing.T, steps ...step) (*app, *scriptedModel, int) {
	t.Helper()
	m := newScriptedModel(steps...)
	a := aiApp(t, m)
	id := publishedIndexed(t, a, a.user("alice"))
	admin := a.admin()
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("the index is %q", s)
	}
	return a, m, id
}

func TestConceptsOfOneNameOrAliasAreMergedAtTheLessonDefiningThem(t *testing.T) {
	t.Parallel()
	a, _, id := builtWith(t,
		of(l00, concepts(as("use", concept("意图识别", "为什么学 Agent", "开篇提到的意图识别。")))),
		of(l01, concepts(as("use", concept("Intent Recognition", "规范化", "规范化之后做的事。", "意图识别")))),
		of(l02, concepts(as("define", concept("意图识别", "三层意图", "分领域、意图、槽位三层。", "Intent")))),
	)

	cs := indexConcepts(t, a.admin(), id)

	if len(cs) != 1 {
		t.Fatalf("one concept: %v", cs)
	}
	c := cs[0]
	if c["name"] != "意图识别" || c["lessons"] != 3.0 || len(c["occurrences"].([]any)) != 3 {
		t.Fatalf("merged: %v", c)
	}
	if aliases := fmt.Sprint(c["aliases"]); !strings.Contains(aliases, "Intent Recognition") || !strings.Contains(aliases, "Intent") {
		t.Fatalf("the merged names are its aliases: %v", aliases)
	}
	p := c["primary"].(map[string]any)
	if p["lesson"].(map[string]any)["key"] != "l02" || sectionTitle(p) != "三层意图" || c["summary"] != "分领域、意图、槽位三层。" {
		t.Fatalf("the primary section is where it is defined: %v", c)
	}
}

func TestTheLessonTitledByAConceptIsItsPrimarySection(t *testing.T) {
	t.Parallel()
	a, _, id := builtWith(t,
		of(l00, concepts(as("define", concept("规范化", "为什么学 Agent", "开篇讲的规范化。")))),
		of(l01, concepts(as("define", concept("规范化", "规范化", "把用户的话整理成统一的形式。")))),
		of(l02, concepts()),
	)

	c := conceptNamed(t, indexConcepts(t, a.admin(), id), "规范化")

	if p := c["primary"].(map[string]any); p["lesson"].(map[string]any)["key"] != "l01" {
		t.Fatalf("of two that define it, the lesson titled by it (01｜用户输入规范化): %v", p)
	}
}

func TestConceptsLinkedOnlyThroughAThirdConceptsAliasesStayApart(t *testing.T) {
	t.Parallel()
	a, _, id := builtWith(t,
		of(l00, concepts(concept("控制面", "为什么学 Agent", "管理节点的部分。"))),
		of(l01, concepts(concept("数据面", "规范化", "转发流量的部分。"))),
		of(l02, concepts(concept("机械态", "三层意图", "两个面合在一起。", "控制面", "数据面"))),
	)

	cs := indexConcepts(t, a.admin(), id)

	var names []string
	for _, c := range cs {
		names = append(names, c["name"].(string))
	}
	slices.Sort(names)
	if fmt.Sprint(names) != "[控制面 数据面]" {
		t.Fatalf("机械态 joins one of them, which stay two concepts: %v", names)
	}
}

func TestNamesDoNotChainFromAliasToAlias(t *testing.T) {
	t.Parallel()
	a, _, id := builtWith(t,
		of(l00, concepts(concept("甲", "为什么学 Agent", "第一个。", "乙"))),
		of(l01, concepts(concept("乙", "规范化", "第二个。", "丙"))),
		of(l02, concepts(concept("丙", "三层意图", "第三个。"))),
	)

	cs := indexConcepts(t, a.admin(), id)

	if len(cs) != 2 {
		t.Fatalf("甲 joins 乙, which then joins no one: %v", cs)
	}
	conceptNamed(t, cs, "丙")
}

func TestAGenericConceptIsLeftOutOfRecall(t *testing.T) {
	t.Parallel()
	a, _, id := builtWith(t,
		of(l00, concepts(concept("模型", "为什么学 Agent", "大模型。"), concept("Agent", "为什么学 Agent", "能自己规划步骤的程序。"))),
		of(l01, concepts(concept("模型", "规范化", "大模型。"))),
		of(l02, concepts(concept("模型", "三层意图", "大模型。"))),
	)
	admin := a.admin()

	if c := conceptNamed(t, indexConcepts(t, admin, id), "模型"); c["generic"] != true {
		t.Fatalf("a concept of every lesson is generic: %v", c)
	}
	var named []string
	for _, h := range recallPathHits(t, recallTest(t, admin, id, "模型和 Agent", ""), "concept") {
		named = append(named, h["label"].(string))
	}
	if fmt.Sprint(named) != "[Agent]" {
		t.Fatalf("the concept path leaves 模型 out: %v", named)
	}
}

func TestAfterExtractingTheIndexWritesTheSummaryTree(t *testing.T) {
	t.Parallel()
	a, m, id := builtWith(t,
		of(l00, lessonOf("开篇讲为什么学 Agent。", concept("Agent", "为什么学 Agent", "能自己规划步骤的程序。"))),
		of(l01, lessonOf("这一讲讲输入规范化。")),
		of(l02, lessonOf("这一讲讲三层意图识别。")),
	)
	admin := a.admin()

	tree := admin.mustOK(admin.do("GET", conceptIndexPath(id)+"/tree", nil)).json()

	if tree["summary"] != "摘要。" {
		t.Fatalf("the course has its summary: %v", tree)
	}
	chapters := tree["chapters"].([]any)
	if len(chapters) != 2 {
		t.Fatalf("the column's two chapters: %v", chapters)
	}
	intent := chapters[1].(map[string]any)
	lessons := intent["lessons"].([]any)
	if intent["title"] != "意图识别" || intent["summary"] != "摘要。" || intent["tooLong"] != false || len(lessons) != 2 ||
		lessons[1].(map[string]any)["summary"] != "这一讲讲三层意图识别。" {
		t.Fatalf("a chapter with its lessons: %v", intent)
	}
	calls := m.summaryCalls()
	if len(calls) != 3 {
		t.Fatalf("a summary for each chapter, then the course: %d", len(calls))
	}
	if asked := calls[1].Messages[1].Content; !strings.Contains(asked, "一章：意图识别") || !strings.Contains(asked, "这一讲讲输入规范化。") || strings.Contains(asked, "开篇讲为什么学 Agent") {
		t.Fatalf("a chapter is summarized from its own lessons: %q", asked)
	}
	if asked := calls[2].Messages[1].Content; !strings.Contains(asked, "整门课程") || !strings.Contains(asked, "### 意图识别\n摘要。") {
		t.Fatalf("the course is summarized from its chapters: %q", asked)
	}
	if x := showIndex(t, admin, id); x["stage"] != "summarize" {
		t.Fatalf("the build went through its summary stage: %v", x["stage"])
	}
}

func TestRebuildingWritesOnlyTheSummariesWhoseInputChanged(t *testing.T) {
	t.Parallel()
	steps := []step{
		of(l00, lessonOf("开篇讲为什么学 Agent。")),
		of(l01, lessonOf("这一讲讲输入规范化。")),
		of(l02, lessonOf("这一讲讲三层意图识别。")),
		of(l01, lessonOf("这一讲改讲输入清洗。")),
	}
	m := newScriptedModel(steps...)
	// each summary tells its input apart, so a chapter's changes with its lessons
	m.summary = func(req assistant.Request) (string, error) {
		return fmt.Sprintf("摘要 %x", sha256.Sum256([]byte(req.Messages[1].Content)))[:16], nil
	}
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))
	awaitIndex(t, admin, id)

	admin.mustOK(admin.do("POST", retryPath(id, "l01"), nil))
	awaitIndex(t, admin, id)

	calls := m.summaryCalls()
	if len(calls) != 5 {
		t.Fatalf("three summaries, then the changed chapter's and the course's again: %d", len(calls))
	}
	if asked := calls[3].Messages[1].Content; !strings.Contains(asked, "这一讲改讲输入清洗。") {
		t.Fatalf("the changed chapter is summarized again: %q", asked)
	}
	if asked := calls[4].Messages[1].Content; !strings.Contains(asked, "整门课程") {
		t.Fatalf("then the course: %q", asked)
	}
}

func TestASummaryThatFailsIsRecordedAndTheIndexStillBuilt(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(
		of(l00, lessonOf("开篇讲为什么学 Agent。")),
		of(l01, lessonOf("这一讲讲输入规范化。")),
		of(l02, lessonOf("这一讲讲三层意图识别。")),
	)
	m.summary = func(req assistant.Request) (string, error) {
		if strings.Contains(req.Messages[1].Content, "一章：开篇") {
			return "", &assistant.Error{Category: assistant.ErrUnavailable, Detail: "down"}
		}
		return "摘要。", nil
	}
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))

	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("a summary failing leaves the index %q", s)
	}
	tree := admin.mustOK(admin.do("GET", conceptIndexPath(id)+"/tree", nil)).json()
	opening := tree["chapters"].([]any)[0].(map[string]any)
	if opening["summary"] != "" || opening["error"] == nil {
		t.Fatalf("the failed chapter keeps why: %v", opening)
	}
	if tree["summary"] != "摘要。" {
		t.Fatalf("the course is summarized from the chapters that were: %v", tree)
	}
}

func TestWhileSummarizingTheAdminSeesTheSummaryStage(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	m := newScriptedModel(
		of(l00, lessonOf("开篇讲为什么学 Agent。")),
		of(l01, lessonOf("这一讲讲输入规范化。")),
		of(l02, lessonOf("这一讲讲三层意图识别。")),
	)
	m.summary = func(assistant.Request) (string, error) {
		<-release
		return "摘要。", nil
	}
	a := aiApp(t, m)
	admin := a.admin()
	id := publishedIndexed(t, a, a.user("alice"))
	admin.mustOK(admin.do("POST", conceptIndexPath(id), nil))

	var x map[string]any
	for range 500 {
		if x = showIndex(t, admin, id); x["stage"] == "summarize" {
			break
		}
	}
	close(release)

	if x["status"] != "building" || x["stage"] != "summarize" || x["progress"].(map[string]any)["done"] != 3.0 {
		t.Fatalf("every lesson extracted, the build summarizes: %v", x)
	}
	if s := awaitIndex(t, admin, id); s != "built" {
		t.Fatalf("the index is %q", s)
	}
}
