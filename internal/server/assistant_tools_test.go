package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

var toolCallIDs atomic.Int64

// call is a tool call with the given arguments (a string is sent as is,
// anything else as JSON).
func call(name string, args any) assistant.ToolCall {
	s, ok := args.(string)
	if !ok {
		b, _ := json.Marshal(args)
		s = string(b)
	}
	return assistant.ToolCall{ID: fmt.Sprintf("call_%d", toolCallIDs.Add(1)), Name: name, Arguments: s}
}

// toolStep is a step that ends with the given tool calls, after the text
// chunks if any.
func toolStep(calls []assistant.ToolCall, chunks ...string) step {
	s := texts(chunks...)
	for _, c := range calls {
		s.events = append(s.events, assistant.Event{Kind: assistant.EventToolCall, ToolCall: &c})
	}
	s.result = assistant.Result{Finish: assistant.FinishToolCalls}
	return s
}

// toolResult is what a request passed back for a tool call.
func toolResult(t *testing.T, req assistant.Request, c assistant.ToolCall) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == assistant.RoleTool && m.ToolCallID == c.ID {
			return m.Content
		}
	}
	t.Fatalf("no result for %s in %+v", c.Name, req.Messages)
	return ""
}

func activities(events []sseEvent) []map[string]any {
	var out []map[string]any
	for _, e := range events {
		if e.name == "activity" && e.data["kind"] != "thinking" {
			out = append(out, e.data)
		}
	}
	return out
}

// toolColumn is analysisColumn with more lessons: l02 has nested headings,
// l03 is upcoming, l02's text can be replaced.
func toolColumn(t *testing.T, c *client, l02 string) (courseID, lessonID int) {
	t.Helper()
	m := columnManifest()
	m["completion"] = "ongoing"
	chapters := m["chapters"].([]any)
	intent := chapters[1].(map[string]any)
	intent["lessons"] = append(intent["lessons"].([]any), map[string]any{"id": "l03", "title": "03｜待更新"})
	files := columnFiles(m)
	files["01_用户输入.md"] = "# 01｜用户输入规范化\n\n" +
		"Agent 不是一个新概念，但在大模型出现之后，它第一次变得**真正可用**。\n\n" +
		"## 一个最小的例子\n\n" +
		"这里的 Agent 指的是把目标拆成步骤的程序。\n"
	if l02 == "" {
		l02 = "# 02｜三层意图识别\n\n## 意图的三层\n\n第一层是 Agent 的入口。\n\n### 细分\n\n更细的 agent 意图。\n\n## 小结\n\n结束。\n"
	}
	files["02_意图识别.md"] = l02
	courseID = uploadCourse(t, c, files)
	return courseID, lessonIDs(t, c, courseID)["l01"]
}

func TestCourseToolsReadTheReadersCourse(t *testing.T) {
	t.Parallel()
	list := call("list_sections", map[string]any{"lesson": "l02"})
	read := call("read_lesson", map[string]any{"lesson": "l02", "section": "意图的三层"})
	search := call("search_course", map[string]any{"query": "agent"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{list, read, search}), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done, got %+v", last)
	}
	calls := m.calls()
	if len(calls) != 2 {
		t.Fatalf("expected a step with tools, then the answer; got %d calls", len(calls))
	}
	var names []string
	for _, tool := range calls[0].Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "read_lesson,list_sections,search_course" || calls[0].ToolChoice != "auto" {
		t.Fatalf("the course tools are declared, the model choosing: %v %q", names, calls[0].ToolChoice)
	}
	if !strings.Contains(string(calls[0].Tools[0].Parameters), `"lesson"`) {
		t.Fatalf("read_lesson declares its parameters: %s", calls[0].Tools[0].Parameters)
	}
	if ctx := calls[0].Messages[1].Content; !strings.Contains(ctx, "01｜用户输入规范化 [l01]") || !strings.Contains(ctx, "本讲 [l01]") {
		t.Fatalf("the outline gives each lesson's identifier:\n%s", ctx)
	}

	sections := toolResult(t, calls[1], list)
	for _, want := range []string{"[l02]", "- [意图的三层] 意图的三层", "\n  - [细分] 细分", "- [小结] 小结"} {
		if !strings.Contains(sections, want) {
			t.Fatalf("list_sections lacks %q:\n%s", want, sections)
		}
	}
	section := toolResult(t, calls[1], read)
	if !strings.Contains(section, "第一层是 Agent 的入口") || !strings.Contains(section, "更细的 agent 意图") || strings.Contains(section, "结束。") {
		t.Fatalf("read_lesson reads the section with what is nested in it, and no further:\n%s", section)
	}
	if !strings.Contains(section, "[l02]") || !strings.Contains(section, "[意图的三层]") {
		t.Fatalf("read_lesson names the lesson and section it read:\n%s", section)
	}
	hits := toolResult(t, calls[1], search)
	for _, want := range []string{"出现 4 处", "讲 [l01]「01｜用户输入规范化」› 小节 [一个最小的例子]", "讲 [l02]「02｜三层意图识别」› 小节 [细分]「细分」：…"} {
		if !strings.Contains(hits, want) {
			t.Fatalf("search_course lacks %q:\n%s", want, hits)
		}
	}

	byID := map[string][]string{}
	targets := map[string]string{}
	for _, act := range activities(events) {
		id := act["id"].(string)
		byID[id] = append(byID[id], act["status"].(string))
		targets[act["kind"].(string)], _ = act["target"].(string)
	}
	if len(byID) != 3 {
		t.Fatalf("one activity per tool call, got %v", byID)
	}
	for id, statuses := range byID {
		if strings.Join(statuses, ",") != "running,done" {
			t.Fatalf("activity %s went %v", id, statuses)
		}
	}
	want := map[string]string{"list_sections": "02｜三层意图识别", "read_lesson": "02｜三层意图识别 › 意图的三层", "search_course": "agent"}
	for k, v := range want {
		if targets[k] != v {
			t.Fatalf("activity target of %s = %q, want %q", k, targets[k], v)
		}
	}
}

func TestCourseToolsSayHowToFixAWrongCall(t *testing.T) {
	t.Parallel()
	noLesson := call("read_lesson", map[string]any{"lesson": "l99"})
	noSection := call("read_lesson", map[string]any{"lesson": "l02", "section": "不存在"})
	upcoming := call("list_sections", map[string]any{"lesson": "l03"})
	badArgs := call("read_lesson", `{"lessn": "l02"}`)
	m := newScriptedModel(toolStep([]assistant.ToolCall{noLesson, noSection, upcoming, badArgs}), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("a wrong call does not end the run, got %+v", last)
	}
	next := m.calls()[1]
	for c, want := range map[*assistant.ToolCall]string{&noLesson: "课程目录", &noSection: "list_sections", &upcoming: "还没有上线", &badArgs: "参数"} {
		if got := toolResult(t, next, *c); !strings.Contains(got, want) {
			t.Fatalf("%s(%s) should say %q, got %q", c.Name, c.Arguments, want, got)
		}
	}
	reasons := map[string]string{}
	for _, act := range activities(events) {
		if act["status"] == "failed" {
			target, _ := act["target"].(string)
			reasons[target] = act["reason"].(string)
		}
	}
	if reasons["l99"] != "not_found" || reasons["02｜三层意图识别 › 不存在"] != "not_found" || reasons["03｜待更新"] != "not_found" || reasons[""] != "invalid_request" {
		t.Fatalf("each wrong call fails with its reason, got %v", reasons)
	}
}

func TestCourseToolsReadOnlyTheVersionTheReaderSees(t *testing.T) {
	t.Parallel()
	other := call("read_lesson", map[string]any{"lesson": "x9"})
	notKept := call("read_lesson", map[string]any{"lesson": "l02"})
	searchOther := call("search_course", map[string]any{"query": "别处独有"})
	searchNotKept := call("search_course", map[string]any{"query": "你好"})
	kept := call("read_lesson", map[string]any{"lesson": "l01"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{other, notKept, searchOther, searchNotKept, kept}), texts(goodAnswer))
	a := aiApp(t, m)
	alice, bob, id, lessons := subscriberWithDoneLesson(t, a)
	files := columnFiles(map[string]any{"version": 1, "title": "另一门课", "type": "column", "chapters": []any{
		map[string]any{"id": "c", "title": "章", "lessons": []any{map[string]any{"id": "x9", "title": "别处", "file": "x.md"}}},
	}})
	files["x.md"] = "# 别处\n\n别处独有的话。\n"
	uploadCourse(t, alice, files)
	unpublish(t, a, id) // bob keeps only l01, the lesson he finished

	events, _ := analyze(t, bob, id, lessons["l01"], map[string]any{"quote": "官网", "prefix": "见", "suffix": "。"})
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done, got %+v", last)
	}
	next := m.calls()[1]
	if got := toolResult(t, next, other); !strings.Contains(got, "没有标识为") {
		t.Fatalf("a lesson of another course is not found, got %q", got)
	}
	if got := toolResult(t, next, notKept); !strings.Contains(got, "看不到") {
		t.Fatalf("a lesson bob did not keep cannot be read, got %q", got)
	}
	for _, c := range []assistant.ToolCall{searchOther, searchNotKept} {
		if got := toolResult(t, next, c); !strings.Contains(got, "没有找到") {
			t.Fatalf("search_course(%s) looks only at what bob can read, got %q", c.Arguments, got)
		}
	}
	if got := toolResult(t, next, kept); !strings.Contains(got, "见[官网](https://example.com)") {
		t.Fatalf("the kept lesson reads as bob sees it, got %q", got)
	}
}

func TestALongLessonIsCutAndTheModelToldToReadItBySection(t *testing.T) {
	t.Parallel()
	long := "# 02\n\n## 很长\n\n" + strings.Repeat("长", 31_000) + "\n"
	read := call("read_lesson", map[string]any{"lesson": "l02"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{read}), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, long)

	analyze(t, bob, id, lesson, agentQuote)
	got := toolResult(t, m.calls()[1], read)
	if strings.Count(got, "长") > 30_000 || !strings.Contains(got, "list_sections") || utf8.RuneCountInString(got) > 30_200 {
		t.Fatalf("expected the text cut at 30,000 characters with a hint, got %d characters ending %q", utf8.RuneCountInString(got), got[len(got)-200:])
	}
}

func TestAfterSixStepsWithToolsTheRunAnswersWithoutThem(t *testing.T) {
	t.Parallel()
	var steps []step
	for i := range 6 {
		steps = append(steps, toolStep([]assistant.ToolCall{call("search_course", map[string]any{"query": fmt.Sprintf("词%d", i)})}))
	}
	m := newScriptedModel(append(steps, texts(goodAnswer))...)
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || last.data["title"] != "Agent（智能体）" {
		t.Fatalf("the reader gets the whole answer, got %+v", last)
	}
	calls := m.calls()
	if len(calls) != 7 {
		t.Fatalf("expected six steps and the answer, got %d calls", len(calls))
	}
	lastNote := func(req assistant.Request) string { return req.Messages[len(req.Messages)-1].Content }
	for i, req := range calls[:6] {
		if req.ToolChoice != "auto" {
			t.Fatalf("call %d may use tools, got %q", i+1, req.ToolChoice)
		}
		if hinted := strings.Contains(lastNote(req), "还剩 1 次查询"); hinted != (i == 5) {
			t.Fatalf("only the last call with tools says one is left; call %d ends %q", i+1, lastNote(req))
		}
	}
	answer := calls[6]
	if answer.ToolChoice != "none" || len(answer.Tools) != 3 {
		t.Fatalf("the answer call keeps the tools declared but may not use them: %q %d", answer.ToolChoice, len(answer.Tools))
	}
	if !strings.Contains(lastNote(answer), "查询次数已用完") {
		t.Fatalf("the answer call says why, got %q", lastNote(answer))
	}
}

func TestAStepCarriesOutAtMostFiveToolCalls(t *testing.T) {
	t.Parallel()
	var cs []assistant.ToolCall
	for i := range 7 {
		cs = append(cs, call("search_course", map[string]any{"query": fmt.Sprintf("词%d", i)}))
	}
	m := newScriptedModel(toolStep(cs), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	next := m.calls()[1]
	for i, c := range cs {
		got := toolResult(t, next, c)
		if skipped := strings.Contains(got, "本步最多 5 个"); skipped != (i >= 5) {
			t.Fatalf("call %d: %q", i+1, got)
		}
	}
	running := 0
	for _, act := range activities(events) {
		if act["status"] == "running" {
			running++
		}
	}
	if running != 5 {
		t.Fatalf("only the five carried out show, got %d", running)
	}
}

func TestAfterTheSoftDeadlineTheRunAnswersWithoutTools(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	first := toolStep([]assistant.ToolCall{call("search_course", map[string]any{"query": "Agent"})})
	first.before = func() { clock.advance(95 * time.Second) }
	m := newScriptedModel(first, texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done, got %+v", last)
	}
	calls := m.calls()
	if len(calls) != 2 || calls[1].ToolChoice != "none" {
		t.Fatalf("past 90 s no call may use tools, got %d calls, the last %q", len(calls), calls[len(calls)-1].ToolChoice)
	}
	if got := calls[1].Messages[len(calls[1].Messages)-1].Content; !strings.Contains(got, "时间快到了") {
		t.Fatalf("the answer call says why, got %q", got)
	}
}

func TestAtTheHardDeadlineTheRunTimesOut(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	first := toolStep([]assistant.ToolCall{call("search_course", map[string]any{"query": "Agent"})}, "<exp_title>Agent</exp_title>")
	first.before = func() { clock.advance(121 * time.Second) }
	m := newScriptedModel(first, texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "error" || last.data["code"] != "timeout" {
		t.Fatalf("expected timeout, got %+v", last)
	}
	if len(m.calls()) != 1 {
		t.Fatalf("nothing is called after the deadline, got %d calls", len(m.calls()))
	}
}

func TestTheDeadlineCanBeSet(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Now()}
	first := toolStep([]assistant.ToolCall{call("search_course", map[string]any{"query": "Agent"})})
	first.before = func() { clock.advance(31 * time.Second) }
	m := newScriptedModel(first, texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now; o.RunDeadline = 40 * time.Second })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	analyze(t, bob, id, lesson, agentQuote)
	if calls := m.calls(); len(calls) != 2 || calls[1].ToolChoice != "none" {
		t.Fatalf("with a 40 s deadline the soft one is at 30 s, got %+v", calls)
	}
}

// countingTool is a test tool that fails with err (or panics with it) and
// counts how often it ran.
func countingTool(name string, fail func() error) (assistant.Tool, *atomic.Int64) {
	var n atomic.Int64
	return assistant.NewTool(name, "a test tool", func(struct{}) string { return name },
		func(context.Context, struct{}) (string, error) {
			n.Add(1)
			return "", fail()
		}), &n
}

func TestAToolPanickingIsToldAsAnInternalErrorAndTheRunGoesOn(t *testing.T) {
	t.Parallel()
	boom, _ := countingTool("boom", func() error { panic("database on fire: secret detail") })
	c := call("boom", "{}")
	m := newScriptedModel(toolStep([]assistant.ToolCall{c}), texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.ExtraTools = []assistant.Tool{boom} })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("the run goes on after a panic, got %+v", last)
	}
	got := toolResult(t, m.calls()[1], c)
	if !strings.Contains(got, "内部出错") || strings.Contains(got, "secret") {
		t.Fatalf("the model hears only that it failed, got %q", got)
	}
	acts := activities(events)
	if len(acts) != 2 || acts[1]["status"] != "failed" || acts[1]["reason"] != "internal" {
		t.Fatalf("the activity fails as internal, got %v", acts)
	}
	// the server still serves
	events, _ = analyze(t, bob, id, lesson, agentQuote)
	if len(events) == 0 {
		t.Fatal("the server stopped serving")
	}
}

func TestATransientFailureIsTriedOnceMoreAndTwiceInARowTurnsTheToolOff(t *testing.T) {
	t.Parallel()
	flaky, tries := countingTool("flaky", func() error {
		return assistant.Transient(assistant.ErrUnavailable, fmt.Errorf("502"))
	})
	var cs []assistant.ToolCall
	var steps []step
	for range 3 {
		c := call("flaky", "{}")
		cs = append(cs, c)
		steps = append(steps, toolStep([]assistant.ToolCall{c}))
	}
	m := newScriptedModel(append(steps, texts(goodAnswer))...)
	a := aiAppWith(t, m, false, func(o *server.Options) { o.ExtraTools = []assistant.Tool{flaky} })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done, got %+v", last)
	}
	if tries.Load() != 4 {
		t.Fatalf("each of two calls is tried twice, then the tool is off: %d tries", tries.Load())
	}
	calls := m.calls()
	if len(calls) != 4 {
		t.Fatalf("retries take no step of their own, got %d calls", len(calls))
	}
	if got := toolResult(t, calls[3], cs[2]); !strings.Contains(got, "不可用") {
		t.Fatalf("a call to a tool turned off is answered without running it, got %q", got)
	}
	failed := 0
	for _, act := range activities(events) {
		if act["status"] == "failed" {
			failed++
			if act["reason"] != "unavailable" {
				t.Fatalf("failed with %v", act)
			}
		}
	}
	if failed != 2 {
		t.Fatalf("the call cut short does not show, got %d failures", failed)
	}
}

func TestAnAccountFailureTurnsTheToolOffAtOnce(t *testing.T) {
	t.Parallel()
	broke, tries := countingTool("paid", func() error {
		return assistant.AccountFailure(assistant.ErrQuotaExhausted, fmt.Errorf("402"))
	})
	first, second := call("paid", "{}"), call("paid", "{}")
	m := newScriptedModel(toolStep([]assistant.ToolCall{first}), toolStep([]assistant.ToolCall{second}), texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.ExtraTools = []assistant.Tool{broke} })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || tries.Load() != 1 {
		t.Fatalf("expected done after one try, got %+v after %d tries", last, tries.Load())
	}
	if got := toolResult(t, m.calls()[2], second); !strings.Contains(got, "不可用") {
		t.Fatalf("got %q", got)
	}
}

func TestADraftFollowedByToolCallsIsReset(t *testing.T) {
	t.Parallel()
	c := call("search_course", map[string]any{"query": "Agent"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{c}, "<exp_title>草稿标题</exp_title><exp_concept>草稿"), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	reset := -1
	for i, e := range events {
		if e.name == "delta" && e.data["reset"] == true {
			if reset >= 0 {
				t.Fatal("one reset expected")
			}
			reset = i
		}
	}
	if reset < 0 {
		t.Fatalf("expected a reset, got %+v", events)
	}
	if got := streamed(events[:reset]); got["title"] != "草稿标题" {
		t.Fatalf("the draft streamed first, got %q", got)
	}
	if got := streamed(events[reset:]); got["title"] != "Agent（智能体）" {
		t.Fatalf("the answer streams after the reset, got %q", got)
	}
	if acts := activities(events[reset:]); len(acts) != 2 {
		t.Fatalf("the lookups show after the reset, got %v", acts)
	}
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done, got %+v", last)
	}
}

func TestToolCallsWithoutADraftSendNoReset(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(toolStep([]assistant.ToolCall{call("search_course", map[string]any{"query": "Agent"})}, "我先查一下。"), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	for _, e := range events {
		if e.name == "delta" && e.data["reset"] == true {
			t.Fatalf("nothing streamed, so nothing to reset: %+v", events)
		}
	}
}

func TestAToolWhoseTargetPanicsDoesNotStopTheServer(t *testing.T) {
	t.Parallel()
	odd := assistant.NewTool("odd", "a test tool", func(struct{}) string { panic("no target") },
		func(context.Context, struct{}) (string, error) { return "结果", nil })
	c := call("odd", "{}")
	m := newScriptedModel(toolStep([]assistant.ToolCall{c}), texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.ExtraTools = []assistant.Tool{odd} })
	bob := a.user("bob")
	id, lesson := toolColumn(t, bob, "")

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || toolResult(t, m.calls()[1], c) != "结果" {
		t.Fatalf("the call runs without a target, got %+v", last)
	}
}
