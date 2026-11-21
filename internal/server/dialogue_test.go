package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func TestAFollowUpReadsTheSnapshotTheFirstReplyTheDocumentThenTheDialogue(t *testing.T) {
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
	want := []string{"system:", "user:", "assistant:", "user:【当前探索文档】", "user:planner 和 executor 怎么分工？"}
	if len(first) != len(want) {
		t.Fatalf("expected %d messages, got %q", len(want), first)
	}
	for i, w := range want {
		if !strings.HasPrefix(first[i], w) {
			t.Fatalf("message %d: expected %q…, got %q", i, w, first[i])
		}
	}
	if !strings.Contains(first[0], "探索对话里继续追问") || !strings.Contains(first[1], "这里的 <selected>Agent</selected> 指的是") ||
		!strings.Contains(first[2], "<exp_title>Agent（智能体）</exp_title>") || !strings.Contains(first[3], "# Agent（智能体）") {
		t.Fatalf("context %q", first)
	}
	// the second turn's request starts as the first one's did, then the first turn
	for i := 0; i < len(first)-1; i++ {
		if second[i] != first[i] {
			t.Fatalf("message %d changed between turns:\n%q\n%q", i, first[i], second[i])
		}
	}
	if rest := second[len(first)-1:]; len(rest) != 3 || rest[0] != "user:planner 和 executor 怎么分工？" || rest[1] != "assistant:第一个后续回复。" || rest[2] != "user:那出错了怎么办？" {
		t.Fatalf("history %q", rest)
	}
}

func TestAFollowUpReadsTheSnapshotAfterTheCourseChanged(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, _, exp := pinnedExploration(t, bob)
	bob.mustOK(updateCourse(t, bob, id, analysisFiles(strings.Replace(analysisLesson, "把目标拆成步骤的程序", "会自己调用工具的程序", 1))))

	ask(t, bob, exp, firstTurn(t, bob, exp), "再说说？")

	snapshot := contents(m.calls()[1])[1]
	if !strings.Contains(snapshot, "把目标拆成步骤的程序") || strings.Contains(snapshot, "会自己调用工具的程序") {
		t.Fatalf("the follow-up reads the text as pinned: %s", snapshot)
	}
}

func TestRunningOutOfCreditsMidReplyKeepsWhatStreamed(t *testing.T) {
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
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	onPlan(t, a, bob, 0)

	_, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns", exp), map[string]any{"after": firstTurn(t, bob, exp), "question": question("还有吗？")})
	expectError(t, r, http.StatusPaymentRequired, "credits_insufficient")
}

func TestAFailedTurnTriedAgainIsReplacedInPlace(t *testing.T) {
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
	m := newScriptedModel(texts(goodAnswer), texts("回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	ask(t, bob, exp, first, "第一问")

	_, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns", exp), map[string]any{"after": first, "question": question("又从第一轮问")})
	expectError(t, r, http.StatusConflict, "not_last_turn")
}
