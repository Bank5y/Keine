package server_test

import (
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

// goodReport is a report as the model writes it: the four parts in their
// tags.
const goodReport = "<rep_title>Agent 的分工</rep_title>\n" +
	"<rep_summary>Agent 把目标拆成步骤。planner 规划，executor 执行。出错时回到规划。</rep_summary>\n" +
	"<rep_body>\n## 原理\n\nAgent 是把目标拆成步骤、逐步执行的系统。\n\n## 机制\n\nplanner 和 executor 分工。\n</rep_body>\n" +
	"<rep_points>\n- 目标拆成步骤\n- 规划与执行分开\n</rep_points>"

func reportsPath(exp int) string { return fmt.Sprintf("/api/explorations/%d/reports", exp) }

// generate generates a report of exp and returns the stream's events and
// the last of them.
func generate(t *testing.T, c *client, exp int) ([]sseEvent, sseEvent) {
	t.Helper()
	events, r := postEvents(t, c, reportsPath(exp), map[string]any{})
	expectStatus(t, r, http.StatusOK)
	return events, lastEvent(t, events)
}

// report reads version v of exp's report.
func report(t *testing.T, c *client, exp, v int) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", fmt.Sprintf("%s/%d", reportsPath(exp), v), nil)).json()
}

func ints(list any) []int {
	var out []int
	for _, x := range list.([]any) {
		out = append(out, int(x.(float64)))
	}
	return out
}

func TestEachReportIsANewVersionAndTheOldOnesStay(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("第一个后续回复。"), texts(cut(goodReport, 30, 90)...), texts("第二个后续回复。"),
		texts(strings.Replace(goodReport, "Agent 的分工", "Agent 的分工与出错", 1)))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	t1, _ := ask(t, bob, exp, first, "planner 和 executor 怎么分工？")

	events, done := generate(t, bob, exp)
	if done.name != "done" || done.data["version"] != 1.0 || done.data["id"] == nil {
		t.Fatalf("expected version 1 done, got %+v", done)
	}
	var streamed []string
	for _, e := range events {
		if e.name == "delta" {
			streamed = append(streamed, e.data["section"].(string))
		}
	}
	if !slices.Contains(streamed, "summary") || !slices.Contains(streamed, "points") {
		t.Fatalf("the parts stream as they are written: %v", streamed)
	}
	t2, _ := ask(t, bob, exp, idOf(t1), "那出错了怎么办？")
	if _, done := generate(t, bob, exp); done.data["version"] != 2.0 {
		t.Fatalf("expected version 2, got %+v", done)
	}

	list := bob.mustOK(bob.do("GET", reportsPath(exp), nil)).list("reports")
	if len(list) != 2 || list[0].(map[string]any)["version"] != 2.0 || list[1].(map[string]any)["version"] != 1.0 {
		t.Fatalf("the versions, newest first: %v", list)
	}
	v1, v2 := report(t, bob, exp, 1), report(t, bob, exp, 2)
	if v1["title"] != "Agent 的分工" || !strings.Contains(v1["summary"].(string), "planner 规划") ||
		!strings.Contains(v1["body"].(string), "## 机制") || !strings.Contains(v1["points"].(string), "规划与执行分开") {
		t.Fatalf("version 1 %v", v1)
	}
	if got := ints(v1["turnIds"]); !slices.Equal(got, []int{first, idOf(t1)}) {
		t.Fatalf("version 1 took in turns %v", got)
	}
	if v2["title"] != "Agent 的分工与出错" || !slices.Equal(ints(v2["turnIds"]), []int{first, idOf(t1), idOf(t2)}) {
		t.Fatalf("version 2 %v", v2)
	}
	expectError(t, bob.do("GET", fmt.Sprintf("%s/3", reportsPath(exp)), nil), http.StatusNotFound, "report_not_found")
	alice := a.user("alice")
	expectError(t, alice.do("GET", fmt.Sprintf("%s/1", reportsPath(exp)), nil), http.StatusNotFound, "exploration_not_found")
}

// turnCount is how many nodes and turns the exploration's dialogue has.
func turnCount(t *testing.T, c *client, exp int) (nodes, turns int) {
	t.Helper()
	d := detail(t, c, exp)
	for _, n := range d["nodes"].([]any) {
		turns += len(n.(map[string]any)["turns"].([]any))
	}
	return len(d["nodes"].([]any)), turns
}

// reportMetas reads, past the API, what each version's run recorded.
func reportMetas(t *testing.T, a *app, exp int) []map[string]any {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT meta FROM exploration_reports WHERE exploration_id = $1 ORDER BY version`, exp)
	if err != nil {
		t.Fatalf("reports: %v", err)
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

func TestAReportReadsTheWholeTreeAndIsNoTurn(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer), texts("主节点的答"), texts("子节点的答"), texts("子节点接着的答"), texts("分支的答"), texts("发散的答"), texts(goodReport))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	t2, _ := ask(t, bob, exp, first, "主节点的问")
	child := openNode(t, bob, exp, idOf(t2), "child", "子节点的问")
	ask(t, bob, exp, idOf(turnsOf(child)[0]), "子节点接着的问")
	openNode(t, bob, exp, first, "branch", "分支的问")
	openNode(t, bob, exp, idOf(turnsOf(child)[0]), "divergent", "发散的问", "子节点的答")
	nodes, turns := turnCount(t, bob, exp)

	if _, done := generate(t, bob, exp); done.name != "done" {
		t.Fatalf("expected the report done, got %+v", done)
	}

	calls := m.calls()
	req := calls[len(calls)-1]
	if len(req.Tools) != 0 {
		t.Fatalf("a report looks nothing up: %v", declared(req))
	}
	msgs := contents(req)
	if !strings.Contains(msgs[0], "<rep_summary>") || !strings.Contains(msgs[0], assistant.DefaultStyle.Text) {
		t.Fatalf("system prompt %q", msgs[0])
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"## 课程信息", "解释「Agent（智能体）」的含义", "Agent 是把目标拆成步骤、逐步执行的系统。",
		"主节点的问", "主节点的答", "子节点的问", "子节点的答", "子节点接着的问", "子节点接着的答", "分支的问", "分支的答", "发散的问", "发散的答",
		"分支节点", "发散节点"} {
		if !strings.Contains(all, want) {
			t.Fatalf("the report's request has no %q:\n%s", want, all)
		}
	}
	for _, gone := range []string{"<exp_", "## 本讲全文", "## 课程目录"} {
		if strings.Contains(all, gone) {
			t.Fatalf("%q is not in a report's request", gone)
		}
	}
	if n, tt := turnCount(t, bob, exp); n != nodes || tt != turns {
		t.Fatalf("the dialogue gained something: %d nodes %d turns, was %d %d", n, tt, nodes, turns)
	}
	if got := ints(report(t, bob, exp, 1)["turnIds"]); len(got) != turns {
		t.Fatalf("the report took in %v, of %d turns", got, turns)
	}
	metas := reportMetas(t, a, exp)
	if metas[0]["prompt"] != "report-1" || metas[0]["style"] != "computing" || metas[0]["styleVersion"] != 1.0 {
		t.Fatalf("the report records its prompt and style: %v", metas[0])
	}
}

func TestAReportsReferencesAreOnlyWhatTheTurnsLookedUp(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	search := call("web_search", map[string]any{"query": "Raft 任期"})
	written := strings.Replace(goodReport, "planner 和 executor 分工。", "planner 和 executor 分工，见 [编造的资料](https://made.up/paper)。\n\n## 参考\n\n- [另一份编造的](https://made.up/other)", 1)
	m := newScriptedModel(texts(goodAnswer), toolStep([]assistant.ToolCall{search}), texts("查过之后的回复。"), texts(written))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	ask(t, bob, exp, firstTurn(t, bob, exp), "Raft 的任期是什么？")

	if _, done := generate(t, bob, exp); done.name != "done" {
		t.Fatalf("expected the report done, got %+v", done)
	}

	refs := report(t, bob, exp, 1)["references"].([]any)
	var urls []string
	for _, r := range refs {
		urls = append(urls, r.(map[string]any)["url"].(string))
	}
	if !slices.Equal(urls, []string{"https://raft.github.io/raft.pdf", "https://example.com/term"}) {
		t.Fatalf("references %v", refs)
	}
	if refs[0].(map[string]any)["title"] != "Raft 论文" {
		t.Fatalf("a reference is titled as its source: %v", refs[0])
	}
	md := bob.mustOK(bob.do("GET", fmt.Sprintf("%s/1/markdown", reportsPath(exp)), nil))
	if !strings.HasPrefix(md.header.Get("Content-Type"), "text/markdown") || !strings.Contains(md.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download headers %v", md.header)
	}
	text := string(md.body)
	refsAt := strings.LastIndex(text, "## 参考")
	if refsAt < 0 || !strings.HasPrefix(text, "# Agent 的分工\n") {
		t.Fatalf("markdown:\n%s", text)
	}
	last := text[refsAt:]
	if !strings.Contains(last, "- [Raft 论文](https://raft.github.io/raft.pdf)") || strings.Contains(last, "made.up") {
		t.Fatalf("the references section is the sources only:\n%s", last)
	}
}

func TestAReportOfATreeTooLongIsRefusedAndCostsNothing(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("长", 1500)
	m := newScriptedModel(texts(goodAnswer), texts(long), texts(long), texts(long))
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	mapModel(t, a, map[string]any{"searchRoute": "off", "contextWindow": 8192 + 4000}, false)
	bob := a.user("bob")
	onPlan(t, a, bob, 100)
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	for _, k := range []string{"child", "branch", "divergent"} {
		openNode(t, bob, exp, first, k, "问")
	}
	before, calls := credits(t, bob)["remaining"], len(m.calls())

	_, r := postEvents(t, bob, reportsPath(exp), map[string]any{})
	expectError(t, r, http.StatusUnprocessableEntity, "report_too_long")
	if !strings.Contains(r.json()["error"].(map[string]any)["message"].(string), "对话过长") {
		t.Fatalf("message %v", r.json())
	}
	if len(m.calls()) != calls {
		t.Fatal("the model was called")
	}
	if after := credits(t, bob)["remaining"]; after != before {
		t.Fatalf("credits went from %v to %v", before, after)
	}
	if list := bob.mustOK(bob.do("GET", reportsPath(exp), nil)).list("reports"); len(list) != 0 {
		t.Fatalf("no report is kept: %v", list)
	}
}

func TestAReportIsChargedAsAReport(t *testing.T) {
	t.Parallel()
	s := texts(goodReport)
	s.result.Usage = assistant.Usage{Input: n64(1_000_000), CacheRead: n64(0), CacheWrite: n64(0), Output: n64(500_000)}
	m := newScriptedModel(texts(goodAnswer), s)
	a := aiApp(t, m)
	bob := a.user("bob")
	onPlan(t, a, bob, 100)
	_, _, exp := pinnedExploration(t, bob)
	before := credits(t, bob)["remaining"].(float64)

	generate(t, bob, exp)

	var reports []map[string]any
	for _, r := range runsOf(t, a, bob) {
		if r := r.(map[string]any); r["kind"] == "report" {
			reports = append(reports, r)
		}
	}
	// 1M input at 2 a million and 0.5M output at 8 a million
	if len(reports) != 1 || !near(reports[0]["credits"], 6) {
		t.Fatalf("usage rows of kind report: %v", runsOf(t, a, bob))
	}
	if after := credits(t, bob)["remaining"].(float64); !near(after, before-6) {
		t.Fatalf("credits went from %v to %v", before, after)
	}
}

func TestAReportThatMayCostMoreThanIsLeftDoesNotStart(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	onPlan(t, a, bob, 1)

	_, r := postEvents(t, bob, reportsPath(exp), map[string]any{})
	expectError(t, r, http.StatusPaymentRequired, "credits_insufficient")
	if msg := r.json()["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "生成报告预计用") {
		t.Fatalf("message %q", msg)
	}
	if len(m.calls()) != 1 {
		t.Fatal("the model was called")
	}
}
