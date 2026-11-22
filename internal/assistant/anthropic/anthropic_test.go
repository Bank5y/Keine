package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/assistant/anthropic"
)

// replay serves recorded streams from testdata, one per request in order,
// and keeps the request bodies it got.
type replay struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	keys   []string
}

func newReplay(t *testing.T, recordings ...string) *replay {
	t.Helper()
	var streams [][]byte
	for _, r := range recordings {
		b, err := os.ReadFile(filepath.Join("testdata", r))
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, b)
	}
	rp := &replay{}
	rp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		rp.mu.Lock()
		n := len(rp.bodies)
		rp.bodies = append(rp.bodies, body)
		rp.keys = append(rp.keys, r.Header.Get("X-Api-Key")+r.Header.Get("Authorization"))
		rp.mu.Unlock()
		if n >= len(streams) {
			http.Error(w, `{"type":"error","error":{"type":"api_error","message":"no more recordings"}}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(streams[n])
	}))
	t.Cleanup(rp.srv.Close)
	return rp
}

func (rp *replay) adapter() *anthropic.Adapter {
	return anthropic.New(assistant.Endpoint{Format: assistant.FormatAnthropic, BaseURL: rp.srv.URL, Key: "sk-ant-test"})
}

func (rp *replay) requests() []map[string]any {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return append([]map[string]any(nil), rp.bodies...)
}

// collect runs one call and returns its events.
func collect(t *testing.T, a assistant.Adapter, req assistant.Request) ([]assistant.Event, assistant.Result, error) {
	t.Helper()
	var events []assistant.Event
	res, err := a.Stream(context.Background(), req, func(e assistant.Event) error {
		events = append(events, e)
		return nil
	})
	return events, res, err
}

func text(events []assistant.Event, kind assistant.EventKind) string {
	s := ""
	for _, e := range events {
		if e.Kind == kind {
			s += e.Text
		}
	}
	return s
}

func n(v int64) *int64 { return &v }

func eqUsage(t *testing.T, got assistant.Usage, want assistant.Usage) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("usage = %s, want %s", g, w)
	}
}

// jsonOf renders v for comparisons and failure messages.
func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var hello = assistant.Request{Model: "claude-opus-5-5", Messages: []assistant.Message{
	{Role: assistant.RoleSystem, Content: "你是助手。"},
	{Role: assistant.RoleUser, Content: "hi"},
}}

func TestTextStreamWithCacheUsage(t *testing.T) {
	rp := newReplay(t, "text.sse")
	events, res, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if got := text(events, assistant.EventText); got != "你好，世界。" {
		t.Errorf("text = %q", got)
	}
	if res.Finish != assistant.FinishStop {
		t.Errorf("finish = %q", res.Finish)
	}
	// input_tokens already leaves out the cache reads and writes
	eqUsage(t, res.Usage, assistant.Usage{Input: n(20), Output: n(7), CacheRead: n(3000), CacheWrite: n(1500)})
	req := rp.requests()[0]
	if rp.keys[0] != "sk-ant-test" && rp.keys[0] != "Bearer sk-ant-test" {
		t.Errorf("auth header = %q", rp.keys[0])
	}
	if req["model"] != "claude-opus-5-5" || req["stream"] != true {
		t.Errorf("request = %v", req)
	}
	if req["max_tokens"] == nil {
		t.Error("max_tokens is required by the API and was not sent")
	}
	sys, _ := req["system"].([]any)
	if len(sys) != 1 || sys[0].(map[string]any)["text"] != "你是助手。" {
		t.Errorf("system = %s", jsonOf(req["system"]))
	}
	msgs, _ := req["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
		t.Errorf("messages = %s", jsonOf(req["messages"]))
	}
	// the cache marks: the whole prompt so far is a cache prefix
	if cc, _ := req["cache_control"].(map[string]any); cc["type"] != "ephemeral" {
		t.Errorf("cache_control = %s", jsonOf(req["cache_control"]))
	}
}

func calls(events []assistant.Event) []assistant.ToolCall {
	var out []assistant.ToolCall
	for _, e := range events {
		if e.Kind == assistant.EventToolCall {
			out = append(out, *e.ToolCall)
		}
	}
	return out
}

var readLesson = assistant.ToolSpec{Name: "read_lesson", Description: "读一讲", Parameters: json.RawMessage(`{"type":"object","properties":{"lesson":{"type":"string"}},"required":["lesson"]}`)}

func TestToolCallsAreHandedBackWhole(t *testing.T) {
	rp := newReplay(t, "tool_calls.sse")
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.ToolChoice = "auto"
	events, res, err := collect(t, rp.adapter(), req)
	if err != nil {
		t.Fatal(err)
	}
	got := calls(events)
	want := []assistant.ToolCall{
		{ID: "toolu_01", Name: "read_lesson", Arguments: `{"lesson": "l02"}`},
		{ID: "toolu_02", Name: "list_sections", Arguments: `{}`},
	}
	if jsonOf(got) != jsonOf(want) {
		t.Errorf("calls = %s", jsonOf(got))
	}
	if res.Finish != assistant.FinishToolCalls {
		t.Errorf("finish = %q", res.Finish)
	}
	body := rp.requests()[0]
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %s", jsonOf(body["tools"]))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "read_lesson" || tool["description"] != "读一讲" || jsonOf(tool["input_schema"]) != `{"properties":{"lesson":{"type":"string"}},"required":["lesson"],"type":"object"}` {
		t.Errorf("tool = %s", jsonOf(tool))
	}
	if tc, _ := body["tool_choice"].(map[string]any); tc["type"] != "auto" {
		t.Errorf("tool_choice = %s", jsonOf(body["tool_choice"]))
	}
}

func TestAnAnsweringCallKeepsTheToolsButMayNotUseThem(t *testing.T) {
	rp := newReplay(t, "text.sse")
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.ToolChoice = "none"
	if _, _, err := collect(t, rp.adapter(), req); err != nil {
		t.Fatal(err)
	}
	body := rp.requests()[0]
	if tools, _ := body["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools = %s", jsonOf(body["tools"]))
	}
	if tc, _ := body["tool_choice"].(map[string]any); tc["type"] != "none" {
		t.Errorf("tool_choice = %s", jsonOf(body["tool_choice"]))
	}
}

func TestARequiredToolChoiceMustCallATool(t *testing.T) {
	rp := newReplay(t, "text.sse")
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.ToolChoice = "required"
	if _, _, err := collect(t, rp.adapter(), req); err != nil {
		t.Fatal(err)
	}
	if tc, _ := rp.requests()[0]["tool_choice"].(map[string]any); tc["type"] != "any" {
		t.Errorf("tool_choice = %s", jsonOf(rp.requests()[0]["tool_choice"]))
	}
}

func TestToolResultsGoBackInOneUserTurn(t *testing.T) {
	rp := newReplay(t, "text.sse")
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.Messages = append(append([]assistant.Message(nil), hello.Messages...),
		assistant.Message{Role: assistant.RoleAssistant, Content: "先查两讲。", ToolCalls: []assistant.ToolCall{
			{ID: "toolu_01", Name: "read_lesson", Arguments: `{"lesson":"l02"}`},
			{ID: "toolu_02", Name: "list_sections", Arguments: `{}`},
		}},
		assistant.Message{Role: assistant.RoleTool, ToolCallID: "toolu_01", Content: "第二讲正文"},
		assistant.Message{Role: assistant.RoleTool, ToolCallID: "toolu_02", Content: "小节目录"},
		assistant.Message{Role: assistant.RoleUser, Content: "还剩 1 次查询。"},
	)
	if _, _, err := collect(t, rp.adapter(), req); err != nil {
		t.Fatal(err)
	}
	want := `[{"content":[{"text":"hi","type":"text"}],"role":"user"},` +
		`{"content":[{"text":"先查两讲。","type":"text"},{"id":"toolu_01","input":{"lesson":"l02"},"name":"read_lesson","type":"tool_use"},{"id":"toolu_02","input":{},"name":"list_sections","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"content":[{"text":"第二讲正文","type":"text"}],"is_error":false,"tool_use_id":"toolu_01","type":"tool_result"},{"content":[{"text":"小节目录","type":"text"}],"is_error":false,"tool_use_id":"toolu_02","type":"tool_result"},{"text":"还剩 1 次查询。","type":"text"}],"role":"user"}]`
	if got := jsonOf(withoutCacheMarks(rp.requests()[0]["messages"])); got != want {
		t.Errorf("messages =\n%s\nwant\n%s", got, want)
	}
}

// withoutCacheMarks drops the cache_control fields, which are not what a
// test about the conversation is about.
func withoutCacheMarks(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if k != "cache_control" {
				out[k] = withoutCacheMarks(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withoutCacheMarks(e)
		}
		return out
	}
	return v
}

func TestThinkingGoesBackWithItsSignatureWithinTheRun(t *testing.T) {
	rp := newReplay(t, "thinking_tool.sse", "text.sse")
	a := rp.adapter()
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.ToolChoice = "auto"
	req.Thinking = true
	events, res, err := collect(t, a, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := text(events, assistant.EventReasoning); got != "需要先读第二讲。" {
		t.Errorf("reasoning = %q", got)
	}
	c := calls(events)
	if len(c) != 1 {
		t.Fatalf("calls = %s", jsonOf(c))
	}
	// the next call of the run passes the message back as it was
	req.Messages = append(append([]assistant.Message(nil), hello.Messages...),
		assistant.Message{Role: assistant.RoleAssistant, ToolCalls: c, Native: res.Native},
		assistant.Message{Role: assistant.RoleTool, ToolCallID: c[0].ID, Content: "第二讲正文"},
	)
	if _, _, err := collect(t, a, req); err != nil {
		t.Fatal(err)
	}
	first, second := rp.requests()[0], rp.requests()[1]
	if th, _ := first["thinking"].(map[string]any); th["type"] != "adaptive" {
		t.Errorf("thinking = %s", jsonOf(first["thinking"]))
	}
	msgs := withoutCacheMarks(second["messages"]).([]any)
	want := `{"content":[{"signature":"EqQBCgIYAhIM1gbcDa9GJwZA2b3hGgxBdjrkzLoky3dl1pkiMOYds","thinking":"需要先读第二讲。","type":"thinking"},{"id":"toolu_03","input":{"lesson":"l02"},"name":"read_lesson","type":"tool_use"}],"role":"assistant"}`
	if len(msgs) != 3 || jsonOf(msgs[1]) != want {
		t.Errorf("messages = %s", jsonOf(msgs))
	}
}

func TestThinkingSwitchedOffSendsNoThinking(t *testing.T) {
	rp := newReplay(t, "text.sse")
	if _, _, err := collect(t, rp.adapter(), hello); err != nil {
		t.Fatal(err)
	}
	if th, ok := rp.requests()[0]["thinking"]; ok {
		t.Errorf("thinking = %s", jsonOf(th))
	}
}

// webEvents is what the provider's own search came to: each step as
// "id kind target status reason", and the sources in order.
func webEvents(events []assistant.Event) (steps []string, sources []assistant.WebSource) {
	for _, e := range events {
		if e.Kind == assistant.EventWeb {
			w := e.Web
			steps = append(steps, strings.TrimSpace(w.ID+" "+w.Kind+" "+w.Target+" "+string(w.Status)+" "+string(w.Reason)))
		}
		if e.Kind == assistant.EventWeb || e.Kind == assistant.EventSources {
			sources = append(sources, e.Sources...)
		}
	}
	return steps, sources
}

func TestTheProvidersSearchBecomesStepsAndSources(t *testing.T) {
	rp := newReplay(t, "web_search.sse")
	req := hello
	req.Tools = []assistant.ToolSpec{readLesson}
	req.ToolChoice = "auto"
	req.ProviderWeb = true
	events, res, err := collect(t, rp.adapter(), req)
	if err != nil {
		t.Fatal(err)
	}
	steps, sources := webEvents(events)
	wantSteps := []string{
		"srvtoolu_01 web_search Raft 任期 running",
		"srvtoolu_01 web_search Raft 任期 done",
		"srvtoolu_02 web_fetch raft.github.io running",
		"srvtoolu_02 web_fetch raft.github.io done",
	}
	if jsonOf(steps) != jsonOf(wantSteps) {
		t.Errorf("steps = %s", jsonOf(steps))
	}
	wantSources := []assistant.WebSource{
		{Title: "Raft 论文", URL: "https://raft.github.io/raft.pdf", Kind: assistant.WebSourceSearch},
		{Title: "任期", URL: "https://example.com/term", Kind: assistant.WebSourceSearch},
		{Title: "In Search of an Understandable Consensus Algorithm", URL: "https://www.raft.github.io/raft.pdf", Kind: assistant.WebSourceFetch},
		{Title: "Raft 主页", URL: "https://raft.github.io/", Kind: assistant.WebSourceSearch},
	}
	if jsonOf(sources) != jsonOf(wantSources) {
		t.Errorf("sources = %s", jsonOf(sources))
	}
	if got := text(events, assistant.EventText); got != "任期是 Raft 的逻辑时钟。" {
		t.Errorf("text = %q", got)
	}
	if res.Finish != assistant.FinishStop || len(calls(events)) != 0 {
		t.Errorf("finish = %q, calls = %v", res.Finish, calls(events))
	}
	// one search billed; the fetch costs only its tokens
	eqUsage(t, res.Usage, assistant.Usage{Input: n(400), Output: n(90), CacheRead: n(2000), CacheWrite: n(0), Searches: n(1)})

	tools, _ := rp.requests()[0]["tools"].([]any)
	var types []string
	for _, tl := range tools {
		types = append(types, fmt.Sprint(tl.(map[string]any)["type"]))
	}
	if strings.Join(types, " ") != "<nil> web_search_20260209 web_fetch_20260209" {
		t.Errorf("tool types = %v", types)
	}
	// a fetch reads only what a search of the run found (ADR 0008)
	fetch := tools[2].(map[string]any)
	if got := jsonOf(fetch["url_sources"]); got != `{"client_tool_results":{"type":"none"},"server_tool_results":{"tools":[{"name":"web_search","type":"tool_reference"}],"type":"only"},"user_input":{"type":"none"}}` {
		t.Errorf("url_sources = %s", got)
	}
}

func TestAnOlderModelGetsTheBasicSearchTools(t *testing.T) {
	rp := newReplay(t, "text.sse")
	req := hello
	req.Model = "claude-haiku-4-5"
	req.ProviderWeb = true
	if _, _, err := collect(t, rp.adapter(), req); err != nil {
		t.Fatal(err)
	}
	tools, _ := rp.requests()[0]["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["type"] != "web_search_20250305" || tools[1].(map[string]any)["type"] != "web_fetch_20250910" {
		t.Errorf("tools = %s", jsonOf(tools))
	}
}

func TestTheProvidersSearchErrorsAreFailedSteps(t *testing.T) {
	rp := newReplay(t, "web_error.sse")
	req := hello
	req.ProviderWeb = true
	events, res, err := collect(t, rp.adapter(), req)
	if err != nil {
		t.Fatal(err)
	}
	steps, sources := webEvents(events)
	want := []string{
		"srvtoolu_11 web_search Raft running",
		"srvtoolu_11 web_search Raft failed rate_limited",
		"srvtoolu_12 web_fetch example.com running",
		"srvtoolu_12 web_fetch example.com failed not_found",
	}
	if jsonOf(steps) != jsonOf(want) {
		t.Errorf("steps = %s", jsonOf(steps))
	}
	if len(sources) != 0 {
		t.Errorf("sources = %s", jsonOf(sources))
	}
	// the call itself went fine, and nothing was billed for the searches
	if res.Finish != assistant.FinishStop || res.Usage.Searches != nil {
		t.Errorf("finish = %q, searches = %v", res.Finish, res.Usage.Searches)
	}
	// and it is made once: the provider's own errors are not tried again
	if len(rp.requests()) != 1 {
		t.Errorf("requests = %d", len(rp.requests()))
	}
}

func TestAPausedTurnResumesWithinTheCall(t *testing.T) {
	rp := newReplay(t, "pause.sse", "resumed.sse")
	req := hello
	req.ProviderWeb = true
	events, res, err := collect(t, rp.adapter(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := text(events, assistant.EventText); got != "<exp_title>任期（term）</exp_title>" {
		t.Errorf("text = %q", got)
	}
	if res.Finish != assistant.FinishStop {
		t.Errorf("finish = %q", res.Finish)
	}
	// both requests are billed
	eqUsage(t, res.Usage, assistant.Usage{Input: n(430), Output: n(30), CacheRead: n(500), CacheWrite: n(100), Searches: n(1)})
	reqs := rp.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	// the paused turn goes back as it came, encrypted results and all, and
	// with no message after it
	msgs := withoutCacheMarks(reqs[1]["messages"]).([]any)
	paused := `{"content":[{"text":"\u003cexp_title\u003e任期","type":"text"},` +
		`{"id":"srvtoolu_21","input":{"query":"Raft term"},"name":"web_search","type":"server_tool_use"},` +
		`{"content":[{"encrypted_content":"EqgfENCRYPTED","page_age":null,"title":"Raft","type":"web_search_result","url":"https://raft.github.io/"}],"tool_use_id":"srvtoolu_21","type":"web_search_tool_result"}],"role":"assistant"}`
	if len(msgs) != 2 || jsonOf(msgs[1]) != paused {
		t.Errorf("resumed with %s", jsonOf(msgs))
	}
	// what the run passes back later is the whole turn
	rp2 := newReplay(t, "text.sse")
	req.Messages = append(append([]assistant.Message(nil), hello.Messages...),
		assistant.Message{Role: assistant.RoleAssistant, Content: "<exp_title>任期（term）</exp_title>", Native: res.Native},
		assistant.Message{Role: assistant.RoleUser, Content: "请重新作答。"})
	if _, _, err := collect(t, rp2.adapter(), req); err != nil {
		t.Fatal(err)
	}
	later := withoutCacheMarks(rp2.requests()[0]["messages"]).([]any)
	blocks := later[1].(map[string]any)["content"].([]any)
	var types []string
	for _, b := range blocks {
		types = append(types, b.(map[string]any)["type"].(string))
	}
	if strings.Join(types, " ") != "text server_tool_use web_search_tool_result text" {
		t.Errorf("passed back = %s", jsonOf(later[1]))
	}
}

func TestFailuresAreCategorized(t *testing.T) {
	cases := []struct {
		status int
		kind   string
		want   assistant.Category
	}{
		{401, "authentication_error", assistant.ErrAuth},
		{403, "permission_error", assistant.ErrAuth},
		{402, "billing_error", assistant.ErrQuotaExhausted},
		{400, "billing_error", assistant.ErrQuotaExhausted},
		{404, "not_found_error", assistant.ErrNotFound},
		{429, "rate_limit_error", assistant.ErrRateLimited},
		{529, "overloaded_error", assistant.ErrUnavailable},
		{500, "api_error", assistant.ErrUnavailable},
		{504, "timeout_error", assistant.ErrTimeout},
		{400, "invalid_request_error", assistant.ErrInvalidRequest},
		{413, "request_too_large", assistant.ErrInvalidRequest},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(c.status)
			fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":"sk-ant-test is not right"}}`, c.kind)
		}))
		a := anthropic.New(assistant.Endpoint{Format: assistant.FormatAnthropic, BaseURL: srv.URL, Key: "sk-ant-test"})
		_, _, err := collect(t, a, hello)
		srv.Close()
		if got := assistant.CategoryOf(err); got != c.want {
			t.Errorf("%d %s: category = %q (%v)", c.status, c.kind, got, err)
		}
	}
}

func TestAnErrorInTheMiddleOfTheStreamEndsTheCall(t *testing.T) {
	rp := newReplay(t, "overloaded.sse")
	events, _, err := collect(t, rp.adapter(), hello)
	if got := assistant.CategoryOf(err); got != assistant.ErrUnavailable {
		t.Errorf("category = %q (%v)", got, err)
	}
	if text(events, assistant.EventText) != "你好" {
		t.Errorf("events = %v", events)
	}
}

func TestATurnStillPausedAfterTheResumesIsCutOff(t *testing.T) {
	rp := newReplay(t, "pause.sse", "pause.sse", "pause.sse", "pause.sse", "pause.sse", "pause.sse", "pause.sse")
	req := hello
	req.ProviderWeb = true
	_, res, err := collect(t, rp.adapter(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Finish != assistant.FinishLength {
		t.Errorf("finish = %q", res.Finish)
	}
	if got := len(rp.requests()); got != 6 {
		t.Errorf("requests = %d", got)
	}
}
