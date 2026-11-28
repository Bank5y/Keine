package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

// step is what the scripted model does for one call: stream events, then
// end with a result or fail with err. With hold, it streams and then waits
// for the call to be cancelled. before runs as the call starts (to move a
// clock).
type step struct {
	// lesson, when set, keeps the step for the extraction of the lesson of
	// that title: extractions run side by side, in no set order (#224)
	lesson string
	events []assistant.Event
	result assistant.Result
	err    error
	hold   bool
	before func()
}

// scriptedModel is the model every site model resolves to: each call takes
// the next step of the script and is kept for inspection. Connection tests
// (the admin saving a model) always pass and are not kept; nor do the
// concept index's summaries (#225) take a step: they answer summary (by
// default "摘要。" and the request's title line) and are kept apart.
type scriptedModel struct {
	mu        sync.Mutex
	steps     []step
	requests  []assistant.Request
	cancelled chan struct{}
	summary   func(req assistant.Request) (string, error)
	summaries []assistant.Request
}

func newScriptedModel(steps ...step) *scriptedModel {
	return &scriptedModel{steps: steps, cancelled: make(chan struct{}, 8)}
}

func (m *scriptedModel) Stream(ctx context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	if len(req.Tools) == 1 && req.Tools[0].Name == "connection_check" {
		return assistant.Result{Finish: assistant.FinishStop}, nil
	}
	if len(req.Messages) > 0 && req.Messages[0].Content == assistant.SummaryPrompt.System {
		return m.summarize(req, emit)
	}
	m.mu.Lock()
	m.requests = append(m.requests, req)
	if len(m.steps) == 0 {
		m.mu.Unlock()
		return assistant.Result{}, &assistant.Error{Category: assistant.ErrUnavailable, Detail: "script ran out"}
	}
	at := 0
	if len(req.Messages) > 0 {
		last := req.Messages[len(req.Messages)-1].Content
		at = slices.IndexFunc(m.steps, func(s step) bool { return s.lesson == "" || strings.Contains(last, "› "+s.lesson+"\n") })
		if at < 0 {
			m.mu.Unlock()
			return assistant.Result{}, &assistant.Error{Category: assistant.ErrUnavailable, Detail: "no step for the lesson"}
		}
	}
	s := m.steps[at]
	m.steps = slices.Delete(m.steps, at, at+1)
	m.mu.Unlock()
	if s.before != nil {
		s.before()
	}
	for _, e := range s.events {
		if err := emit(e); err != nil {
			return assistant.Result{}, err
		}
	}
	if s.hold {
		<-ctx.Done()
		m.cancelled <- struct{}{}
		return assistant.Result{}, ctx.Err()
	}
	if s.err != nil {
		return assistant.Result{}, s.err
	}
	if s.result.Finish == "" {
		s.result.Finish = assistant.FinishStop
	}
	return s.result, nil
}

func (m *scriptedModel) summarize(req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	m.mu.Lock()
	m.summaries = append(m.summaries, req)
	write := m.summary
	m.mu.Unlock()
	text, err := "摘要。", error(nil)
	if write != nil {
		text, err = write(req)
	}
	if err != nil {
		return assistant.Result{}, err
	}
	if err := emit(assistant.Event{Kind: assistant.EventText, Text: text}); err != nil {
		return assistant.Result{}, err
	}
	return assistant.Result{Finish: assistant.FinishStop}, nil
}

// summaryCalls are the summary requests the model was sent.
func (m *scriptedModel) summaryCalls() []assistant.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]assistant.Request(nil), m.summaries...)
}

func (m *scriptedModel) calls() []assistant.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]assistant.Request(nil), m.requests...)
}

// texts is a step streaming the given text chunks.
func texts(chunks ...string) step {
	s := step{}
	for _, c := range chunks {
		s.events = append(s.events, assistant.Event{Kind: assistant.EventText, Text: c})
	}
	return s
}

// cut splits s into chunks at the given character positions.
func cut(s string, at ...int) []string {
	r := []rune(s)
	var out []string
	prev := 0
	for _, i := range at {
		out = append(out, string(r[prev:i]))
		prev = i
	}
	return append(out, string(r[prev:]))
}

func withModel(m assistant.Adapter) appOption {
	return func(o *server.Options) {
		o.AdapterFor = func(assistant.Endpoint) assistant.Adapter { return m }
	}
}

// aiApp is an app whose assistant runs on m: an admin has added a site
// model and mapped every turn kind to it.
func aiApp(t *testing.T, m *scriptedModel) *app {
	t.Helper()
	return aiAppThinking(t, m, false)
}

// aiAppThinking is aiApp with thinking switched on or off for every kind.
func aiAppThinking(t *testing.T, m *scriptedModel, thinking bool) *app {
	t.Helper()
	return aiAppWith(t, m, thinking)
}

// aiAppWith is aiAppThinking with more options for the app.
func aiAppWith(t *testing.T, m *scriptedModel, thinking bool, opts ...appOption) *app {
	t.Helper()
	a := newApp(t, append([]appOption{withModel(m), withMasterKey(masterKey)}, opts...)...)
	mapModel(t, a, map[string]any{"searchRoute": "off"}, thinking)
	return a
}

// mapModel adds a site model, the given fields over a plain one, and maps
// every turn kind to it.
func mapModel(t *testing.T, a *app, fields map[string]any, thinking bool) {
	t.Helper()
	admin := a.admin()
	body := map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-test-0123456789", "model": "m-1", "displayName": "Flash", "rates": rates,
	}
	for k, v := range fields {
		body[k] = v
	}
	created := admin.mustOK(admin.do("POST", "/api/admin/models", body)).json()
	for _, k := range []string{"analysis", "followup", "report", "concept_index"} {
		admin.mustOK(admin.do("PUT", "/api/admin/turns/"+k, map[string]any{"modelId": created["id"], "thinking": thinking}))
	}
}

// analysisColumn is a column whose first lesson mentions Agent twice.
func analysisColumn(t *testing.T, c *client) (courseID, lessonID int) {
	t.Helper()
	files := columnFiles(columnManifest())
	files["01_用户输入.md"] = "# 01｜用户输入规范化\n\n" +
		"Agent 不是一个新概念，但在大模型出现之后，它第一次变得**真正可用**。\n\n" +
		"## 一个最小的例子\n\n" +
		"这里的 Agent 指的是把目标拆成步骤的程序。\n"
	courseID = uploadCourse(t, c, files)
	return courseID, lessonIDs(t, c, courseID)["l01"]
}

func analysisPath(courseID, lessonID int) string {
	return fmt.Sprintf("/api/courses/%d/lessons/%d/analysis", courseID, lessonID)
}

type sseEvent struct {
	name string
	data map[string]any
}

// analyze asks for an analysis and reads the whole stream; a JSON answer
// (an error before the run started) comes back as resp instead.
func analyze(t *testing.T, c *client, courseID, lessonID int, body map[string]any) ([]sseEvent, resp) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", c.a.srv.URL+analysisPath(courseID, lessonID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("analysis: %v", err)
	}
	defer res.Body.Close()
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(res.Body)
		return nil, resp{status: res.StatusCode, body: buf.Bytes(), header: res.Header}
	}
	return readEvents(t, bufio.NewScanner(res.Body)), resp{status: res.StatusCode}
}

func readEvents(t *testing.T, sc *bufio.Scanner) []sseEvent {
	t.Helper()
	var out []sseEvent
	var cur sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("event data %q: %v", line, err)
			}
		case line == "" && cur.name != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out
}

// streamed joins the delta text of each section.
func streamed(events []sseEvent) map[string]string {
	out := map[string]string{}
	for _, e := range events {
		if e.name == "delta" {
			out[e.data["section"].(string)] += e.data["text"].(string)
		}
	}
	return out
}

func lastEvent(t *testing.T, events []sseEvent) sseEvent {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	return events[len(events)-1]
}

var agentQuote = map[string]any{"quote": "Agent", "prefix": "这里的 ", "suffix": " 指的是"}

const goodAnswer = "<exp_title>Agent（智能体）</exp_title>\n<exp_concept>能自主规划步骤、调用工具的大模型应用。</exp_concept>\n<exp_explanation>\nAgent 是把目标拆成步骤、逐步执行的系统。\n\n在这一讲里，它指把目标拆成步骤的程序。\n</exp_explanation>"

func TestAnalysisStreamsTheThreeSectionsAndEndsWithThem(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(cut(goodAnswer, 20, 70)...))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, r := analyze(t, bob, id, lesson, agentQuote)
	expectStatus(t, r, http.StatusOK)

	got := streamed(events)
	want := map[string]string{
		"title":       "Agent（智能体）",
		"concept":     "能自主规划步骤、调用工具的大模型应用。",
		"explanation": "Agent 是把目标拆成步骤、逐步执行的系统。\n\n在这一讲里，它指把目标拆成步骤的程序。",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("streamed %s = %q, want %q", k, got[k], v)
		}
	}
	var order []string
	for _, e := range events {
		if e.name == "delta" && (len(order) == 0 || order[len(order)-1] != e.data["section"]) {
			order = append(order, e.data["section"].(string))
		}
	}
	if strings.Join(order, ",") != "title,concept,explanation" {
		t.Fatalf("sections streamed in order %v", order)
	}
	done := lastEvent(t, events)
	if done.name != "done" {
		t.Fatalf("expected done last, got %+v", done)
	}
	for k, v := range want {
		if done.data[k] != v {
			t.Fatalf("done %s = %q, want %q", k, done.data[k], v)
		}
	}
}

func TestTagsCutBetweenChunksAndTextOutsideThemStreamCorrectly(t *testing.T) {
	t.Parallel()
	answer := "好的，下面是分析。\n" + goodAnswer + "\n希望有帮助。"
	// every cut lands inside a tag or right at its edge
	var at []int
	for _, tag := range []string{"<exp_title>", "</exp_title>", "<exp_concept>", "</exp_explanation>"} {
		i := strings.Index(answer, tag)
		at = append(at, len([]rune(answer[:i]))+3)
	}
	m := newScriptedModel(texts(cut(answer, at...)...))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	got := streamed(events)
	if got["title"] != "Agent（智能体）" || got["concept"] != "能自主规划步骤、调用工具的大模型应用。" ||
		got["explanation"] != "Agent 是把目标拆成步骤、逐步执行的系统。\n\n在这一讲里，它指把目标拆成步骤的程序。" {
		t.Fatalf("streamed %q", got)
	}
	for _, e := range events {
		if e.name == "delta" && (strings.Contains(e.data["text"].(string), "下面是分析") || strings.Contains(e.data["text"].(string), "希望")) {
			t.Fatalf("text outside the tags was streamed: %+v", e)
		}
	}
	if lastEvent(t, events).name != "done" {
		t.Fatalf("expected done, got %+v", lastEvent(t, events))
	}
}

func TestTheModelReadsTheCourseThenTheSelectionMarkedInTheLesson(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	calls := m.calls()
	if len(calls) != 1 {
		t.Fatalf("expected one model call, got %d", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) != 2 || msgs[0].Role != assistant.RoleSystem {
		t.Fatalf("expected the analysis prompt then the context, got %+v", msgs)
	}
	for _, tag := range []string{"<exp_title>", "<exp_concept>", "<exp_explanation>"} {
		if !strings.Contains(msgs[0].Content, tag) {
			t.Fatalf("the analysis prompt asks for %s: %q", tag, msgs[0].Content)
		}
	}
	ctx := msgs[1].Content
	order := []string{
		"## 课程信息", "课程：Agent 系统设计", "一门讲 Agent 的专栏。", "大明",
		"## 课程目录", "- 意图识别", "  - 01｜用户输入规范化",
		"## 选中的原文", "位置：意图识别 › 01｜用户输入规范化 › 一个最小的例子", "「Agent」",
		"## 本讲全文",
		"Agent 不是一个新概念", "这里的 <selected>Agent</selected> 指的是",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(ctx[at:], want)
		if i < 0 {
			t.Fatalf("context lacks %q after position %d:\n%s", want, at, ctx)
		}
		at += i + len(want)
	}
	if strings.Count(ctx, "</selected>") != 1 {
		t.Fatalf("exactly one place should be marked:\n%s", ctx)
	}
	if calls[0].MaxTokens != 4096 {
		t.Fatalf("thinking off caps output at 4096, got %d", calls[0].MaxTokens)
	}
}

// answer is a tagged answer with the given parts.
func answer(title, concept, explanation string) string {
	return "<exp_title>" + title + "</exp_title><exp_concept>" + concept + "</exp_concept><exp_explanation>" + explanation + "</exp_explanation>"
}

const okBody = "Agent 是把目标拆成步骤、逐步执行的系统。"

func TestEachValidationRuleRejectsAnAnswer(t *testing.T) {
	t.Parallel()
	long := func(n int) string { return strings.Repeat("字", n) }
	cases := map[string]string{
		"unclosed section":           "<exp_title>Agent</exp_title><exp_concept>概念。</exp_concept><exp_explanation>" + okBody,
		"empty title":                answer(" ", "概念。", okBody),
		"empty concept":              answer("Agent", "\n", okBody),
		"list in concept":            answer("Agent", "- 第一点\n- 第二点", okBody),
		"heading in concept":         answer("Agent", "## 概念", okBody),
		"heading in explanation":     answer("Agent", "概念。", "# 任期\n\n"+okBody),
		"sub-heading in explanation": answer("Agent", "概念。", okBody+"\n\n### 基本规则\n\n- 单调递增"),
		"link in explanation":        answer("Agent", "概念。", okBody+"见[文档](https://example.com/agent)。"),
		"bare url in explanation":    answer("Agent", "概念。", okBody+"见 https://example.com 。"),
		"svg in explanation":         answer("Agent", "概念。", okBody+"\n\n<svg viewBox=\"0 0 10 10\"></svg>"),
		"two diagrams":               answer("Agent", "概念。", okBody+"\n\n```mermaid\nflowchart TD\nA-->B\n```\n\n```mermaid\nsequenceDiagram\nA->>B: x\n```"),
		"sideways diagram":           answer("Agent", "概念。", okBody+"\n\n```mermaid\nflowchart LR\nA-->B\n```"),
		"title too long":             answer(long(101), "概念。", okBody),
		"concept too long":           answer("Agent", long(301), okBody),
		"explanation too long":       answer("Agent", "概念。", long(20001)),
		"raw tool call":              answer("Agent", "概念。", okBody) + "<｜tool▁calls▁begin｜>",
		"section twice":              answer("Agent", "概念。", okBody) + "<exp_title>又一个</exp_title>",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			m := newScriptedModel(texts(bad))
			a := aiApp(t, m)
			bob := a.user("bob")
			id, lesson := analysisColumn(t, bob)

			events, _ := analyze(t, bob, id, lesson, agentQuote)
			last := lastEvent(t, events)
			if last.name != "error" || last.data["code"] != "malformed_output" || last.data["kept"] != true {
				t.Fatalf("expected malformed_output keeping what streamed, got %+v", last)
			}
			if len(m.calls()) != 1 {
				t.Fatalf("an answer the reader saw is not asked for again, got %d calls", len(m.calls()))
			}
		})
	}
}

// A concept explanation may group what it says under bold phrases, each
// with its list, as many groups as the content needs (#157).
func TestBoldPhrasesGroupAConceptExplanation(t *testing.T) {
	t.Parallel()
	explanation := "任期是 Raft 里单调递增的逻辑时钟，把时间切成一段段，每段最多一个 Leader。\n\n" +
		"**基本规则：**\n\n- 每个节点保存当前任期号，持久化存储。\n- 发现更大的任期号时立即更新，并退回 Follower。\n\n" +
		"**关键细节**：\n\n1. 候选人发起选举前先把任期加一。\n2. 带着过期任期的请求直接拒绝。"
	m := newScriptedModel(texts(answer("任期（term）", "Raft 里给每一段 Leader 统治期编号的逻辑时钟。", explanation)))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || last.data["explanation"] != explanation {
		t.Fatalf("expected done with the grouped explanation, got %+v", last)
	}
}

func TestADiagramOfAnAllowedKindIsValid(t *testing.T) {
	t.Parallel()
	body := okBody + "\n\n```mermaid\nstateDiagram-v2\n[*] --> 运行\n```"
	m := newScriptedModel(texts(answer("Agent", "概念。", body)))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || last.data["explanation"] != body {
		t.Fatalf("expected done with the diagram, got %+v", last)
	}
}

func TestACutOffAnswerThatStreamedEndsTruncated(t *testing.T) {
	t.Parallel()
	s := texts("<exp_title>Agent</exp_title><exp_concept>简介。</exp_concept><exp_explanation>Agent 是")
	s.result = assistant.Result{Finish: assistant.FinishLength}
	m := newScriptedModel(s)
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "error" || last.data["code"] != "output_truncated" || last.data["kept"] != true {
		t.Fatalf("expected output_truncated, got %+v", last)
	}
}

func TestAnAnswerInvalidBeforeAnythingStreamedIsAskedForOnceMore(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts("Agent 就是智能体，它会规划步骤。"), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || last.data["title"] != "Agent（智能体）" {
		t.Fatalf("expected the remedied answer, got %+v", last)
	}
	calls := m.calls()
	if len(calls) != 2 {
		t.Fatalf("expected one remedy call, got %d calls", len(calls))
	}
	msgs := calls[1].Messages
	n := len(msgs)
	if n != 4 || msgs[2].Role != assistant.RoleAssistant || msgs[2].Content != "Agent 就是智能体，它会规划步骤。" {
		t.Fatalf("the remedy should carry the invalid answer, got %+v", msgs)
	}
	if msgs[3].Role != assistant.RoleUser || !strings.Contains(msgs[3].Content, "缺少完整的 <exp_title>") {
		t.Fatalf("the remedy should say what was wrong, got %q", msgs[3].Content)
	}
}

func TestARemedyIsTriedOnlyOnce(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts("没有标签。"), texts("还是没有标签。"), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "error" || last.data["code"] != "malformed_output" || last.data["kept"] != false {
		t.Fatalf("expected malformed_output with nothing kept, got %+v", last)
	}
	if len(m.calls()) != 2 {
		t.Fatalf("expected two calls, got %d", len(m.calls()))
	}
}

func unavailable() step {
	return step{err: &assistant.Error{Category: assistant.ErrUnavailable, Detail: "502: bad gateway"}}
}

func TestAProviderFailureBeforeAnyDeltaIsRetriedOnce(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(unavailable(), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("expected done after the retry, got %+v", last)
	}
	if len(m.calls()) != 2 {
		t.Fatalf("expected two calls, got %d", len(m.calls()))
	}
}

func TestAProviderFailingTwiceEndsTheRun(t *testing.T) {
	t.Parallel()
	limited := step{err: &assistant.Error{Category: assistant.ErrRateLimited, Detail: "429"}}
	m := newScriptedModel(limited, limited, texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "error" || last.data["code"] != "rate_limited" || last.data["kept"] != false {
		t.Fatalf("expected rate_limited, got %+v", last)
	}
	if len(m.calls()) != 2 {
		t.Fatalf("expected two calls, got %d", len(m.calls()))
	}
}

func TestAProviderFailureAfterADeltaEndsTheRunKeepingWhatStreamed(t *testing.T) {
	t.Parallel()
	s := texts("<exp_title>Agent</exp_title><exp_concept>能自主")
	s.err = &assistant.Error{Category: assistant.ErrUnavailable, Detail: "connection reset"}
	m := newScriptedModel(s, texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if got := streamed(events); got["title"] != "Agent" || got["concept"] != "能自主" {
		t.Fatalf("streamed %q", got)
	}
	if last := lastEvent(t, events); last.name != "error" || last.data["code"] != "provider_error" || last.data["kept"] != true {
		t.Fatalf("expected provider_error keeping what streamed, got %+v", last)
	} else if msg, _ := last.data["message"].(string); msg == "" || strings.Contains(msg, "connection reset") {
		t.Fatalf("the error should say what happened in the reader's words, not the provider's: %q", msg)
	}
	if len(m.calls()) != 1 {
		t.Fatalf("no retry once something streamed, got %d calls", len(m.calls()))
	}
}

func TestTheReaderGoingAwayCancelsTheModelCall(t *testing.T) {
	t.Parallel()
	s := texts("<exp_title>Agent</exp_title>")
	s.hold = true
	m := newScriptedModel(s)
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	b, _ := json.Marshal(agentQuote)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", a.srv.URL+analysisPath(id, lesson), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := bob.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() && !strings.HasPrefix(sc.Text(), "data: ") {
	}
	cancel()
	res.Body.Close()
	select {
	case <-m.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the model call was not cancelled")
	}
}

func TestAnalysisIsRefusedBeforeItStarts(t *testing.T) {
	t.Parallel()
	m := newScriptedModel()
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	_, r := analyze(t, bob, id, lesson, map[string]any{"quote": "不在正文里的话"})
	expectError(t, r, http.StatusUnprocessableEntity, "quote_not_found")

	video := videoCourse(t, bob)
	_, r = analyze(t, bob, video, lessonIDs(t, bob, video)["page"], map[string]any{"quote": "视频"})
	expectError(t, r, http.StatusConflict, "not_annotatable")

	_, r = analyze(t, bob, id, lesson, map[string]any{"quote": "  "})
	expectStatus(t, r, http.StatusBadRequest)

	carol := a.user("carol")
	_, r = analyze(t, carol, id, lesson, agentQuote)
	expectStatus(t, r, http.StatusNotFound)

	if len(m.calls()) != 0 {
		t.Fatalf("no refused analysis may reach the model, got %d calls", len(m.calls()))
	}
}

func TestAnalysisNeedsTheAssistantAvailable(t *testing.T) {
	t.Parallel()
	a := newApp(t, withModel(newScriptedModel()))
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusConflict, "ai_unavailable")
}

func TestThinkingOnAllowsALongerAnswer(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer))
	a := aiAppThinking(t, m, true)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	if calls := m.calls(); len(calls) != 1 || calls[0].MaxTokens != 8192 {
		t.Fatalf("thinking on caps output at 8192, got %+v", calls)
	}
}

func TestAConnectionTimingOutBeforeAnyDeltaIsRetried(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(step{err: &assistant.Error{Category: assistant.ErrTimeout, Detail: "dial tcp: i/o timeout"}}, texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if last := lastEvent(t, events); last.name != "done" || len(m.calls()) != 2 {
		t.Fatalf("expected done after one retry, got %+v after %d calls", last, len(m.calls()))
	}
}

// thinking puts reasoning before a step's text.
func thinking(s step, reasoning ...string) step {
	var ev []assistant.Event
	for _, r := range reasoning {
		ev = append(ev, assistant.Event{Kind: assistant.EventReasoning, Text: r})
	}
	s.events = append(ev, s.events...)
	return s
}

func TestReasoningShowsAsThinkingAndNeverStreamsAsText(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(thinking(texts(cut(goodAnswer, 30)...), "先想一想任期", "再想想定义"))
	a := aiAppThinking(t, m, true)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	var activity []string
	for _, e := range events {
		switch e.name {
		case "delta":
			if strings.Contains(e.data["text"].(string), "想") {
				t.Fatalf("reasoning streamed as text: %+v", e)
			}
		case "activity":
			if e.data["kind"] != "thinking" || e.data["id"] == "" {
				t.Fatalf("expected a thinking activity, got %+v", e)
			}
			activity = append(activity, e.data["status"].(string))
		}
	}
	if strings.Join(activity, ",") != "running,done" {
		t.Fatalf("thinking should run once and be done, got %v", activity)
	}
	if first := events[0]; first.name != "activity" {
		t.Fatalf("thinking shows before any text, got %+v first", first)
	}
	if lastEvent(t, events).name != "done" || !m.calls()[0].Thinking {
		t.Fatalf("expected done, with thinking asked for: %+v", lastEvent(t, events))
	}
}

func TestReasoningGoesBackWithinARunButNotIntoTheNext(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(thinking(texts("没有标签。"), "第一步的推理"), texts(goodAnswer), texts(goodAnswer))
	a := aiAppThinking(t, m, true)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	calls := m.calls()
	if len(calls) != 2 {
		t.Fatalf("expected a remedy call, got %d calls", len(calls))
	}
	if got := calls[1].Messages[2]; got.Role != assistant.RoleAssistant || got.Reasoning != "第一步的推理" {
		t.Fatalf("the second call of the run should carry the first one's reasoning, got %+v", got)
	}

	analyze(t, bob, id, lesson, agentQuote)
	next := m.calls()[2]
	for _, msg := range next.Messages {
		if msg.Reasoning != "" {
			t.Fatalf("a new run carries no reasoning, got %+v", next.Messages)
		}
	}
}
