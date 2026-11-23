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
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/assistant"
)

// postEvents posts body to path and reads the answer's events; a JSON
// answer (turned away before the run started) comes back as resp instead.
func postEvents(t *testing.T, c *client, path string, body any) ([]sseEvent, resp) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", c.a.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer res.Body.Close()
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(res.Body)
		return nil, resp{status: res.StatusCode, body: buf.Bytes(), header: res.Header}
	}
	return readEvents(t, bufio.NewScanner(res.Body)), resp{status: res.StatusCode}
}

func question(text string, fragment ...string) map[string]any {
	att := []any{}
	for _, f := range fragment {
		att = append(att, map[string]any{"kind": "selection", "text": f})
	}
	return map[string]any{"text": text, "attachments": att}
}

// ask asks a question after turn `after` and returns the turn kept, from
// the run's last event (done, or error with what was kept).
func ask(t *testing.T, c *client, exp int, after int, text string) (map[string]any, sseEvent) {
	t.Helper()
	events, r := postEvents(t, c, fmt.Sprintf("/api/explorations/%d/turns", exp), map[string]any{"after": after, "question": question(text)})
	expectStatus(t, r, http.StatusOK)
	last := lastEvent(t, events)
	kept, _ := last.data["result"].(map[string]any)
	if kept == nil {
		t.Fatalf("expected the turn kept in %+v", last)
	}
	return kept, last
}

func detail(t *testing.T, c *client, exp int) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", explorationPath(exp), nil)).json()
}

// nodeOf is the node of d named, or kind main for "".
func nodeOf(t *testing.T, d map[string]any, id int) map[string]any {
	t.Helper()
	for _, n := range d["nodes"].([]any) {
		if m := n.(map[string]any); int(m["id"].(float64)) == id || (id == 0 && m["kind"] == "main") {
			return m
		}
	}
	t.Fatalf("node %d not in %v", id, d["nodes"])
	return nil
}

func turnsOf(n map[string]any) []map[string]any {
	var out []map[string]any
	for _, t := range n["turns"].([]any) {
		out = append(out, t.(map[string]any))
	}
	return out
}

func idOf(m map[string]any) int { return int(m["id"].(float64)) }

// firstTurn is the analysis turn of an exploration's main node.
func firstTurn(t *testing.T, c *client, exp int) int {
	t.Helper()
	return idOf(turnsOf(nodeOf(t, detail(t, c, exp), 0))[0])
}

func contents(req assistant.Request) []string {
	out := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		out[i] = string(m.Role) + ":" + m.Content
	}
	return out
}

func TestAFollowUpReadsTheBackgroundTheFirstExchangeThenTheDialogue(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("第一个后续回复。"), texts("第二个后续回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	t1, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "planner 和 executor 怎么分工？")
	if t1["reply"] != "第一个后续回复。" || t1["meta"].(map[string]any)["interrupted"] != nil {
		t.Fatalf("kept %v", t1)
	}
	ask(t, bob, exp, idOf(t1), "那出错了怎么办？")

	calls := m.calls()
	first, second := contents(calls[1]), contents(calls[2])
	want := []string{"system:", "user:", "user:解释「Agent（智能体）」的含义", "assistant:", "user:planner 和 executor 怎么分工？"}
	if len(first) != len(want) {
		t.Fatalf("expected %d messages, got %q", len(want), first)
	}
	for i, w := range want {
		if !strings.HasPrefix(first[i], w) {
			t.Fatalf("message %d: expected %q…, got %q", i, w, first[i])
		}
	}
	if first[2] != "user:解释「Agent（智能体）」的含义" {
		t.Fatalf("the first question is a question of its own: %q", first[2])
	}
	background, reply := first[1], first[3]
	if !strings.Contains(background, "读者已经读过") || !strings.Contains(background, "## 课程信息") ||
		!strings.Contains(background, "## 选中的原文") || !strings.Contains(background, "「Agent」") {
		t.Fatalf("background %q", background)
	}
	if !strings.Contains(reply, "Agent（智能体）") || !strings.Contains(reply, "Agent 是把目标拆成步骤、逐步执行的系统。") {
		t.Fatalf("first reply %q", reply)
	}
	for _, msg := range first {
		for _, gone := range []string{"## 课程目录", "本讲全文", "<selected>", "【当前探索文档】", "<exp_"} {
			if strings.Contains(msg, gone) {
				t.Fatalf("%q is not in a follow-up's request: %q", gone, msg)
			}
		}
	}
	// the second turn's request starts as the first one, then its reply
	if len(second) != len(first)+2 || !slices.Equal(second[:len(first)], first) {
		t.Fatalf("the second request does not start with the first:\n%q\n%q", first, second)
	}
	if rest := second[len(first):]; rest[0] != "assistant:第一个后续回复。" || rest[1] != "user:那出错了怎么办？" {
		t.Fatalf("history %q", rest)
	}
}

func TestAnExplorationOfASentenceIsFirstAskedToExplainIt(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)
	sentence := "这里的 Agent 指的是把目标拆成步骤的程序。"
	r := pin(bob, id, lesson, analysisID(t, bob, id, lesson, map[string]any{"quote": sentence}))
	expectStatus(t, r, http.StatusCreated)
	exp := int(r.json()["id"].(float64))

	ask(t, bob, exp, firstTurn(t, bob, exp), "再说说？")

	q := "解释这段话：「" + sentence + "」"
	if got := contents(m.calls()[1])[2]; got != "user:"+q {
		t.Fatalf("expected the first question %q, got %q", q, got)
	}
	if got := detail(t, bob, exp)["firstQuestion"]; got != q {
		t.Fatalf("the detail shows the first question: %v", got)
	}
}

func TestTheDetailShowsTheQuestionOfAnExplorationOfATerm(t *testing.T) {
	t.Parallel()
	bob := aiApp(t, newScriptedModel(texts(goodAnswer))).user("bob")
	_, _, exp := pinnedExploration(t, bob)

	if got := detail(t, bob, exp)["firstQuestion"]; got != "解释「Agent（智能体）」的含义" {
		t.Fatalf("first question %v", got)
	}
}

func TestTheAnalysisAndAFollowUpBothAnswerInTheStyleAndRecordIt(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	ask(t, bob, exp, firstTurn(t, bob, exp), "再说说？")

	style := assistant.DefaultStyle
	for i, call := range m.calls() {
		if system := contents(call)[0]; !strings.Contains(system, style.Text) {
			t.Fatalf("request %d has no answer style in its system prompt: %q", i, system)
		}
	}
	metas := turnMetas(t, a, exp)
	if len(metas) != 2 {
		t.Fatalf("expected 2 turns, got %v", metas)
	}
	for i, want := range []string{"analysis-4", "followup-2"} {
		mt := metas[i]
		if mt["prompt"] != want || mt["style"] != style.ID || mt["styleVersion"] != float64(style.Version) {
			t.Fatalf("turn %d meta %v", i, mt)
		}
	}
	if style.ID != "computing" || style.Version != 1 {
		t.Fatalf("default style %s-%d", style.ID, style.Version)
	}
}

// turnMetas reads, past the API, each turn's whole metadata, in order.
func turnMetas(t *testing.T, a *app, exp int) []map[string]any {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT t.meta FROM exploration_turns t JOIN exploration_nodes n ON n.id = t.node_id
		WHERE n.exploration_id = $1 ORDER BY t.id`, exp)
	if err != nil {
		t.Fatalf("turns: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		var mt map[string]any
		if err := rows.Scan(&raw); err != nil || json.Unmarshal(raw, &mt) != nil {
			t.Fatalf("meta: %v", err)
		}
		out = append(out, mt)
	}
	return out
}

func TestAFollowUpReadsTheBackgroundAsPinnedAfterTheCourseChanged(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, _, exp := pinnedExploration(t, bob)
	manifest := columnManifest()
	manifest["intro"] = "一门改过简介的专栏。"
	files := columnFiles(manifest)
	files["01_用户输入.md"] = analysisLesson
	bob.mustOK(updateCourse(t, bob, id, files))

	ask(t, bob, exp, firstTurn(t, bob, exp), "再说说？")

	background := contents(m.calls()[1])[1]
	if !strings.Contains(background, "一门讲 Agent 的专栏。") || strings.Contains(background, "改过简介") {
		t.Fatalf("the follow-up reads the course as pinned: %s", background)
	}
}

func TestRunningOutOfCreditsMidReplyKeepsWhatStreamed(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("已经流出的一段。", "后面这段不该出现。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	// a sliver left after the analysis: the turn starts, then runs out
	onPlan(t, a, bob, credits(t, bob)["used"].(float64)+0.000001)

	kept, last := ask(t, bob, exp, firstTurn(t, bob, exp), "继续讲讲？")

	if last.name != "error" || last.data["code"] != "credits_exhausted" || last.data["kept"] != true {
		t.Fatalf("expected credits_exhausted, got %+v", last)
	}
	if kept["reply"] != "已经流出的一段。" || kept["meta"].(map[string]any)["interrupted"] != "credits_exhausted" {
		t.Fatalf("expected the streamed part kept as interrupted, got %v", kept)
	}
	if turns := turnsOf(nodeOf(t, detail(t, bob, exp), 0)); len(turns) != 2 || turns[1]["reply"] != "已经流出的一段。" {
		t.Fatalf("kept in the node: %v", turns)
	}
}

func TestAFollowUpNeedsSomeCreditsLeft(t *testing.T) {
	t.Parallel()
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	onPlan(t, a, bob, 0)

	_, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns", exp), map[string]any{"after": firstTurn(t, bob, exp), "question": question("还有吗？")})
	expectError(t, r, http.StatusPaymentRequired, "credits_insufficient")
}

func TestAFailedTurnTriedAgainIsReplacedInPlace(t *testing.T) {
	t.Parallel()
	down := step{err: &assistant.Error{Category: assistant.ErrUnavailable, Detail: "down"}}
	m := newScriptedModel(texts(goodAnswer), down, down, texts("重试后的回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	failed, last := ask(t, bob, exp, firstTurn(t, bob, exp), "这是什么意思？")
	if last.name != "error" || failed["meta"].(map[string]any)["interrupted"] != "failed" || failed["reply"] != "" {
		t.Fatalf("expected a failed turn kept, got %+v / %v", last, failed)
	}

	events, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns/%d/retry", exp, idOf(failed)), map[string]any{})
	expectStatus(t, r, http.StatusOK)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("retry ended with %+v", last)
	}
	turns := turnsOf(nodeOf(t, detail(t, bob, exp), 0))
	if len(turns) != 2 || idOf(turns[1]) != idOf(failed) || turns[1]["reply"] != "重试后的回复。" || turns[1]["meta"].(map[string]any)["interrupted"] != nil {
		t.Fatalf("expected the turn replaced in place, got %v", turns)
	}
	if q := contents(m.calls()[3]); q[len(q)-1] != "user:这是什么意思？" {
		t.Fatalf("the retry asks the same question: %q", q)
	}
}

func TestAKeptTurnHoldsNoToolCallsOrReasoning(t *testing.T) {
	t.Parallel()
	lookup := step{events: []assistant.Event{
		{Kind: assistant.EventReasoning, Text: "先想一想要查什么"},
		{Kind: assistant.EventToolCall, ToolCall: &assistant.ToolCall{ID: "c1", Name: "search_course", Arguments: `{"query":"planner"}`}},
	}}
	m := newScriptedModel(texts(goodAnswer), lookup, texts("查过之后的回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	kept, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "课程里还有哪里讲到？")
	if kept["reply"] != "查过之后的回复。" {
		t.Fatalf("kept %v", kept)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var row string
	if err := conn.QueryRow(ctx, "SELECT question::text || reply || meta::text FROM exploration_turns WHERE id = $1", idOf(kept)).Scan(&row); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if strings.Contains(row, "先想一想") || strings.Contains(row, "search_course") || strings.Contains(row, `"query"`) {
		t.Fatalf("the kept turn holds the run's workings: %s", row)
	}
}

func TestATurnGoesOnlyAfterTheLastOfItsNode(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	ask(t, bob, exp, first, "第一问")

	_, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns", exp), map[string]any{"after": first, "question": question("又从第一轮问")})
	expectError(t, r, http.StatusConflict, "not_last_turn")
}
