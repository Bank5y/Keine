package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

// linkedFiles is a column whose l01 holds the analyzed "Agent" (as
// analysisColumn's) and whose other lessons each have a section for the
// concept index to place entries in.
func linkedFiles() map[string]string {
	files := columnFiles(columnManifest())
	files["00_开篇词.md"] = "# 开篇词\n\n## 为什么学 Agent\n\nAgent 是能自己规划步骤的程序。\n\n## 工具调用\n\n工具调用让模型能执行动作。\n"
	files["01_用户输入.md"] = analysisLesson
	files["02_意图识别.md"] = "# 02\n\n## 三层意图\n\n意图识别分三层：领域、意图、槽位。\n"
	return files
}

// linkedLessons are the extractions of linkedFiles' lessons, l00 with
// extra entries when given.
func linkedLessons(more ...map[string]any) []step {
	l00 := append([]map[string]any{
		concept("工具调用", "工具调用", "让模型执行动作。", "Function Calling"),
	}, more...)
	return []step{
		concepts(l00...),
		concepts(concept("目标拆解", "一个最小的例子", "把目标拆成步骤。")),
		concepts(concept("意图识别", "三层意图", "分领域、意图、槽位三层。", "Intent")),
	}
}

// linkedExploration publishes linkedFiles, has the admin build its index
// (unless not indexed), and has bob analyze and pin "Agent" in l01. The
// model runs the extractions (when indexed) and the analysis.
func linkedExploration(t *testing.T, a *app, indexed bool) (bob *client, courseID, exp int) {
	t.Helper()
	alice := a.user("alice")
	courseID = uploadCourse(t, alice, linkedFiles())
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/courses/%d/publish", courseID), nil))
	if indexed {
		admin.mustOK(admin.do("POST", conceptIndexPath(courseID), nil))
		if s := awaitIndex(t, admin, courseID); s != "built" {
			t.Fatalf("the index is %q", s)
		}
	}
	bob = a.user("bob")
	lesson := lessonIDs(t, bob, courseID)["l01"]
	r := pin(bob, courseID, lesson, analysisID(t, bob, courseID, lesson, agentQuote))
	expectStatus(t, r, http.StatusCreated)
	return bob, courseID, int(r.json()["id"].(float64))
}

// linkSteps is the script up to the fork turn's answer: the extractions
// and the analysis.
func linkSteps(lessons []step, after ...step) []step {
	return append(append(lessons, texts(goodAnswer)), after...)
}

func linkCall(links ...map[string]any) step {
	if links == nil {
		links = []map[string]any{}
	}
	return toolStep([]assistant.ToolCall{call("link_course", map[string]any{"links": links})})
}

func link(entry, note string) map[string]any { return map[string]any{"entry": entry, "note": note} }

// toolNames are the tools a request offered.
func toolNames(req assistant.Request) []string {
	var out []string
	for _, s := range req.Tools {
		out = append(out, s.Name)
	}
	return out
}

func TestAForkAnswerNamingAnotherLessonsConceptIsFollowedByTheCandidates(t *testing.T) {
	t.Parallel()
	const answer = "子节点的回答：目标拆解之后，要先做意图识别，再用 function calling 执行。"
	m := newScriptedModel(linkSteps(linkedLessons(), texts(answer), linkCall())...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	calls := m.calls()
	if len(calls) != 6 {
		t.Fatalf("extractions, analysis, answer, then linking: got %d calls", len(calls))
	}
	linking := calls[5]
	msgs := contents(linking)
	if want := "assistant:" + answer; msgs[len(msgs)-2] != want {
		t.Fatalf("the linking call follows the answer as written: %q", msgs[len(msgs)-2])
	}
	asked := lastQuestion(t, msgs)
	for _, want := range []string{"意图识别", "意图识别分三层：领域、意图、槽位。", "工具调用", "工具调用让模型能执行动作。", "此次作答提到了这些课程内容"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the candidates' message holds %q: %s", want, asked)
		}
	}
	if strings.Contains(asked, "目标拆解") || strings.Contains(asked, "把目标拆成步骤的程序") {
		t.Fatalf("the exploration's own lesson is no candidate: %s", asked)
	}
	if names := toolNames(linking); len(names) != 1 || names[0] != "link_course" {
		t.Fatalf("linking offers link_course only: %v", names)
	}
}

func TestTheLinkingStepMustCallLinkCourse(t *testing.T) {
	t.Parallel()
	const answer = "子节点的回答：先做意图识别。"
	m := newScriptedModel(linkSteps(linkedLessons(), texts(answer), linkCall())...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	calls := m.calls()
	if got := calls[len(calls)-1].ToolChoice; got != "required" {
		t.Fatalf("the linking step's tool choice = %q", got)
	}
}

func TestTheLinksKeptAreTheCandidatesTheModelPickedWithTheirPlaces(t *testing.T) {
	t.Parallel()
	const answer = "子节点的回答：先做意图识别，再用 function calling 执行。"
	m := newScriptedModel(linkSteps(linkedLessons(), texts(answer),
		linkCall(link("c1", "这里的第一步就是那一讲的意图识别。"), link("c9", "编出来的条目"), link("c2", "执行靠工具调用。")))...)
	a := aiApp(t, m)
	bob, courseID, exp := linkedExploration(t, a, true)

	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	ids := lessonIDs(t, bob, courseID)
	want := []map[string]any{
		{"concept": "意图识别", "note": "这里的第一步就是那一讲的意图识别。",
			"lesson":  map[string]any{"id": float64(ids["l02"]), "key": "l02", "title": "02｜三层意图识别"},
			"section": map[string]any{"id": "三层意图", "title": "三层意图"}},
		{"concept": "工具调用", "note": "执行靠工具调用。",
			"lesson":  map[string]any{"id": float64(ids["l00"]), "key": "l00", "title": "开篇词"},
			"section": map[string]any{"id": "工具调用", "title": "工具调用"}},
	}
	for where, turn := range map[string]map[string]any{
		"done":   turnsOf(n)[0],
		"detail": turnsOf(nodeOf(t, detail(t, bob, exp), idOf(n)))[0],
	} {
		if got := fmt.Sprint(turn["courseLinks"]); got != fmt.Sprint(want) {
			t.Fatalf("%s: links\nwant %v\ngot  %v", where, want, got)
		}
		if turn["reply"] != answer {
			t.Fatalf("%s: the answer stays as written: %q", where, turn["reply"])
		}
	}
}

func TestAnEmptyListLinksNothing(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkSteps(linkedLessons(), texts("先做意图识别。"), linkCall())...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "branch", "相关的概念")

	if links := turnsOf(n)[0]["courseLinks"].([]any); len(links) != 0 {
		t.Fatalf("no links: %v", links)
	}
}

// mentioning names a concept of l02 and one of l00.
const mentioning = "回答：先做意图识别，再用 function calling 执行。"

func TestOnlyTheForkTurnIsLinked(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linkCall(link("c1", "相关。")), texts(mentioning), texts(mentioning))...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)
	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	later, last := ask(t, bob, exp, idOf(turnsOf(n)[0]), "再讲讲")
	onMain, mainLast := ask(t, bob, exp, firstTurn(t, bob, exp), "主节点上再问")

	if n := len(m.calls()); n != 8 {
		t.Fatalf("one call for each later turn, none linking: got %d calls", n)
	}
	for _, c := range m.calls()[6:] {
		if names := toolNames(c); len(names) == 1 && names[0] == "link_course" {
			t.Fatalf("a later turn linked: %v", names)
		}
	}
	for _, turn := range []map[string]any{later, onMain} {
		if links := turn["courseLinks"].([]any); len(links) != 0 || turn["reply"] != mentioning {
			t.Fatalf("a later turn ends as it did: %v", turn)
		}
	}
	if last.name != "done" || mainLast.name != "done" {
		t.Fatalf("the turns end with done: %+v %+v", last, mainLast)
	}
}

func TestWithoutAnIndexTheForkTurnEndsWithItsAnswer(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts(mentioning))
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, false)

	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	if c := len(m.calls()); c != 2 {
		t.Fatalf("the analysis and the answer only: %d calls", c)
	}
	if links := turnsOf(n)[0]["courseLinks"].([]any); len(links) != 0 {
		t.Fatalf("no links: %v", links)
	}
}

func TestTheReaderSeesTheLinkingStepAsAnActivityAndNoneOfItsText(t *testing.T) {
	t.Parallel()
	linking := linkCall(link("c1", "相关。"))
	linking.events = append([]assistant.Event{{Kind: assistant.EventText, Text: "我来判断一下。"}}, linking.events...)
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linking)...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	events, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/nodes", exp), map[string]any{"kind": "child", "fork": firstTurn(t, bob, exp), "question": question("往深处讲讲")})
	expectStatus(t, r, http.StatusOK)

	var text string
	var linkStatus []string
	for _, e := range events {
		switch {
		case e.name == "delta":
			text += e.data["text"].(string)
		case e.name == "activity" && e.data["kind"] == "link_course":
			linkStatus = append(linkStatus, e.data["status"].(string))
		}
	}
	if text != mentioning {
		t.Fatalf("the reader sees the answer only: %q", text)
	}
	if fmt.Sprint(linkStatus) != "[running done]" {
		t.Fatalf("link_course runs, then is done: %v", linkStatus)
	}
	if last := lastEvent(t, events); last.name != "done" || len(turnsOf(last.data["result"].(map[string]any))[0]["courseLinks"].([]any)) != 1 {
		t.Fatalf("done carries the links: %+v", last)
	}
}

func TestALinkingStepPastTheDeadlineKeepsTheAnswerWhole(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	linking := linkCall(link("c1", "相关。"))
	linking.before = func() { clock.advance(3 * time.Minute) }
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linking)...)
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob, _, exp := linkedExploration(t, a, true)

	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	turn := turnsOf(nodeOf(t, detail(t, bob, exp), idOf(n)))[0]
	if turn["reply"] != mentioning || turn["meta"].(map[string]any)["interrupted"] != nil || len(turn["courseLinks"].([]any)) != 0 {
		t.Fatalf("the answer is kept whole, unlinked: %v", turn)
	}
}

func TestRunningOutOfCreditsWhileLinkingKeepsTheAnswerWhole(t *testing.T) {
	t.Parallel()
	linking := texts(strings.Repeat("判断", 100_000))
	linking.hold = true
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linking)...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)
	onPlan(t, a, bob, credits(t, bob)["used"].(float64)+0.1)

	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	select {
	case <-m.cancelled:
	default:
		t.Fatal("the linking call was cancelled")
	}
	turn := turnsOf(nodeOf(t, detail(t, bob, exp), idOf(n)))[0]
	if turn["reply"] != mentioning || turn["meta"].(map[string]any)["interrupted"] != nil || len(turn["courseLinks"].([]any)) != 0 {
		t.Fatalf("the answer is kept whole, unlinked: %v", turn)
	}
}

func TestALaterTurnReadsTheForkTurnWithItsLinks(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linkCall(link("c1", "第一步就是意图识别。")), texts("答二"))...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)
	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	ask(t, bob, exp, idOf(turnsOf(n)[0]), "再讲讲")

	msgs := contents(m.calls()[6])
	reply := msgs[len(msgs)-2]
	if !strings.HasPrefix(reply, "assistant:"+mentioning) || !strings.Contains(reply, "课程里的相关内容") ||
		!strings.Contains(reply, "02｜三层意图识别") || !strings.Contains(reply, "第一步就是意图识别。") {
		t.Fatalf("the fork turn's reply comes with its links: %q", reply)
	}
}

func TestTheReportReadsTheForkTurnWithItsLinks(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(linkSteps(linkedLessons(), texts(mentioning), linkCall(link("c1", "第一步就是意图识别。")), texts(goodReport))...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)
	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	if _, done := generate(t, bob, exp); done.name != "done" {
		t.Fatalf("report ended with %+v", done)
	}

	calls := m.calls()
	tree := strings.Join(contents(calls[len(calls)-1]), "\n")
	if !strings.Contains(tree, mentioning) || !strings.Contains(tree, "课程里的相关内容") ||
		!strings.Contains(tree, "02｜三层意图识别") || !strings.Contains(tree, "第一步就是意图识别。") {
		t.Fatalf("the report reads the fork turn's reply with its links: %q", tree)
	}
}

func TestAForkTurnTriedAgainIsLinked(t *testing.T) {
	t.Parallel()
	down := step{err: &assistant.Error{Category: assistant.ErrUnavailable, Detail: "down"}}
	m := newScriptedModel(linkSteps(linkedLessons(), down, down, texts(mentioning), linkCall(link("c1", "相关。")))...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)
	events, _ := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/nodes", exp), map[string]any{"kind": "child", "fork": firstTurn(t, bob, exp), "question": question("往深处讲讲")})
	n := lastEvent(t, events).data["result"].(map[string]any)

	events, _ = postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns/%d/retry", exp, idOf(turnsOf(n)[0])), map[string]any{})

	last := lastEvent(t, events)
	if last.name != "done" || len(last.data["result"].(map[string]any)["courseLinks"].([]any)) != 1 {
		t.Fatalf("the retried fork turn is linked: %+v", last)
	}
}

func TestAtMostFiveCandidatesAreShown(t *testing.T) {
	t.Parallel()
	var more []map[string]any
	var answer strings.Builder
	answer.WriteString("回答：")
	for _, n := range "甲乙丙丁戊己" {
		name := "概念" + string(n)
		more = append(more, concept(name, "为什么学 Agent", "一个概念。"))
		answer.WriteString(name + "，")
	}
	m := newScriptedModel(linkSteps(linkedLessons(more...), texts(answer.String()), linkCall())...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	asked := lastQuestion(t, contents(m.calls()[5]))
	if n := strings.Count(asked, "\n位置："); n != 5 {
		t.Fatalf("five candidates, got %d: %s", n, asked)
	}
	if !strings.Contains(asked, "[c5] 概念戊") || strings.Contains(asked, "概念己") {
		t.Fatalf("the first five the answer names: %s", asked)
	}
}

// candidateNames are the concepts a linking message offers, in its order.
func candidateNames(asked string) []string {
	var out []string
	for _, line := range strings.Split(asked, "\n") {
		if strings.HasPrefix(line, "[c") {
			if _, name, ok := strings.Cut(line, "] "); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

// forkCandidates has bob open a child node whose answer is answer, with
// more entries in l00, and gives the concepts its linking step offered.
func forkCandidates(t *testing.T, answer string, more ...map[string]any) []string {
	t.Helper()
	m := newScriptedModel(linkSteps(linkedLessons(more...), texts(answer), linkCall())...)
	a := aiApp(t, m)
	bob, _, exp := linkedExploration(t, a, true)

	openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	calls := m.calls()
	if len(calls) != 6 {
		t.Fatalf("extractions, analysis, answer, then linking: got %d calls", len(calls))
	}
	return candidateNames(lastQuestion(t, contents(calls[5])))
}

func TestANameOfLettersAndDigitsIsOnlyMatchedAsAWholeWord(t *testing.T) {
	t.Parallel()
	more := []map[string]any{
		concept("4C", "为什么学 Agent", "用户画像的四个维度。"),
		concept("Nagle", "为什么学 Agent", "攒小包再发。"),
	}
	cases := []struct {
		answer string
		want   []string
	}{
		{"回答：G4C 讲的是任务规划，TCP Nagle 算法另说，先做意图识别。", []string{"Nagle", "意图识别"}},
		{"回答：G4C 和用户画像的 4C 不是一回事，先做意图识别。", []string{"4C", "意图识别"}},
	}
	for _, c := range cases {
		got := forkCandidates(t, c.answer, more...)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Fatalf("%q: candidates %v, want %v", c.answer, got, c.want)
		}
	}
}

func TestOfTwoEntriesNamedAtTheSamePlaceOnlyTheLongerNameIsACandidate(t *testing.T) {
	t.Parallel()
	more := []map[string]any{
		concept("日志", "为什么学 Agent", "按时间记下的事件。"),
		concept("日志复制", "为什么学 Agent", "领导者把日志发给跟随者。"),
	}
	got := forkCandidates(t, "回答：Raft 靠日志复制保持一致，先做意图识别。", more...)
	if want := "日志复制|意图识别"; strings.Join(got, "|") != want {
		t.Fatalf("candidates %v, want %s", got, want)
	}
	got = forkCandidates(t, "回答：日志复制之前，日志要先写盘。", more...)
	if want := "日志复制|日志"; strings.Join(got, "|") != want {
		t.Fatalf("a name also standing alone stays a candidate: %v, want %s", got, want)
	}
}
