package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/assistant/openai"
)

// replay serves a recorded stream from testdata and keeps the request it got.
type replay struct {
	srv  *httptest.Server
	body map[string]any
	auth string
	path string
}

func newReplay(t *testing.T, recording string) *replay {
	t.Helper()
	stream, err := os.ReadFile(filepath.Join("testdata", recording))
	if err != nil {
		t.Fatal(err)
	}
	rp := &replay{}
	rp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		rp.path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rp.body)
		rp.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(stream)
	}))
	t.Cleanup(rp.srv.Close)
	return rp
}

func (rp *replay) adapter() *openai.Adapter {
	return openai.New(assistant.Endpoint{Format: assistant.FormatOpenAI, BaseURL: rp.srv.URL + "/v1", Key: "sk-test"})
}

// deepseek is the adapter with the DeepSeek provider configuration.
func (rp *replay) deepseek() *openai.Adapter {
	return openai.New(assistant.Endpoint{Format: assistant.FormatOpenAI, Provider: assistant.ProviderDeepSeek, BaseURL: rp.srv.URL + "/v1", Key: "sk-test"})
}

// official is the adapter with the DeepSeek provider configuration at
// DeepSeek's own address, which the replay serves.
func (rp *replay) official() *openai.Adapter {
	srv, _ := url.Parse(rp.srv.URL)
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = srv.Scheme, srv.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
	base := assistant.DefaultBaseURL(assistant.FormatOpenAI, assistant.ProviderDeepSeek)
	return openai.New(assistant.Endpoint{Format: assistant.FormatOpenAI, Provider: assistant.ProviderDeepSeek, BaseURL: base, Key: "sk-test", Client: client})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

func calls(events []assistant.Event) []assistant.ToolCall {
	var out []assistant.ToolCall
	for _, e := range events {
		if e.Kind == assistant.EventToolCall {
			out = append(out, *e.ToolCall)
		}
	}
	return out
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

var hello = assistant.Request{Model: "m-1", Messages: []assistant.Message{{Role: assistant.RoleUser, Content: "hi"}}}

func TestTextStream(t *testing.T) {
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
	// prompt_tokens 30 of which 12 cached
	eqUsage(t, res.Usage, assistant.Usage{Input: n(18), Output: n(5), CacheRead: n(12)})
	if rp.auth != "Bearer sk-test" {
		t.Errorf("auth header = %q", rp.auth)
	}
	if rp.body["model"] != "m-1" || rp.body["stream"] != true {
		t.Errorf("request = %v", rp.body)
	}
	if so, _ := rp.body["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Errorf("stream_options = %v", rp.body["stream_options"])
	}
}

func eqCalls(t *testing.T, got []assistant.ToolCall, want ...assistant.ToolCall) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("tool calls = %s\nwant %s", g, w)
	}
}

func TestToolCallsAssembledFromFragments(t *testing.T) {
	rp := newReplay(t, "tool_calls.sse")
	events, res, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	eqCalls(t, calls(events),
		assistant.ToolCall{ID: "call_a", Name: "read_lesson", Arguments: `{"lesson":"l-12"}`},
		assistant.ToolCall{ID: "call_b", Name: "search_course", Arguments: `{"q":"闭包"}`})
	if res.Finish != assistant.FinishToolCalls {
		t.Errorf("finish = %q", res.Finish)
	}
	eqUsage(t, res.Usage, assistant.Usage{Input: n(100), Output: n(20)})
}

func TestToolCallNameRepeatedInEveryFragment(t *testing.T) {
	rp := newReplay(t, "repeated_name.sse")
	events, _, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	eqCalls(t, calls(events), assistant.ToolCall{ID: "call_1", Name: "read_lesson", Arguments: `{"lesson":"l-3"}`})
}

func TestToolCallsWithoutIndex(t *testing.T) {
	rp := newReplay(t, "missing_index.sse")
	events, res, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	eqCalls(t, calls(events),
		assistant.ToolCall{ID: "x1", Name: "list_sections", Arguments: `{"lesson":"l-1"}`},
		assistant.ToolCall{ID: "x2", Name: "search_course", Arguments: `{}`})
	if res.Finish != assistant.FinishToolCalls {
		t.Errorf("finish = %q, want tool_calls whatever the endpoint said", res.Finish)
	}
}

func TestCumulativeUsageCountedOnce(t *testing.T) {
	rp := newReplay(t, "cumulative_usage.sse")
	events, res, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if got := text(events, assistant.EventReasoning); got != "先想想。" {
		t.Errorf("reasoning = %q", got)
	}
	if got := text(events, assistant.EventText); got != "答" {
		t.Errorf("text = %q", got)
	}
	eqUsage(t, res.Usage, assistant.Usage{Input: n(40), Output: n(4), Reasoning: n(2)})
	if res.Finish != assistant.FinishLength {
		t.Errorf("finish = %q", res.Finish)
	}
}

// a plain endpoint gets no reasoning back: passing it back is a provider
// configuration's rule (TestDeepSeekSwitchesThinkingAndPassesReasoningBack)
func TestRequestCarriesToolsAndHistory(t *testing.T) {
	rp := newReplay(t, "text.sse")
	req := assistant.Request{
		Model: "m-1",
		Messages: []assistant.Message{
			{Role: assistant.RoleSystem, Content: "sys"},
			{Role: assistant.RoleUser, Content: "q"},
			{Role: assistant.RoleAssistant, Reasoning: "r", ToolCalls: []assistant.ToolCall{{ID: "c1", Name: "read_lesson", Arguments: `{"lesson":"l-1"}`}}},
			{Role: assistant.RoleTool, ToolCallID: "c1", Content: "lesson text"},
		},
		Tools:      []assistant.ToolSpec{{Name: "read_lesson", Description: "Read a lesson", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice: "none",
		MaxTokens:  64,
	}
	if _, _, err := collect(t, rp.adapter(), req); err != nil {
		t.Fatal(err)
	}
	want := `{"max_tokens":64,"messages":[{"content":"sys","role":"system"},{"content":"q","role":"user"},` +
		`{"content":null,"role":"assistant","tool_calls":[{"function":{"arguments":"{\"lesson\":\"l-1\"}","name":"read_lesson"},"id":"c1","type":"function"}]},` +
		`{"content":"lesson text","role":"tool","tool_call_id":"c1"}],"model":"m-1","stream":true,"stream_options":{"include_usage":true},` +
		`"tool_choice":"none","tools":[{"function":{"description":"Read a lesson","name":"read_lesson","parameters":{"type":"object"}},"type":"function"}]}`
	got, _ := json.Marshal(rp.body)
	if string(got) != want {
		t.Errorf("request body =\n%s\nwant\n%s", got, want)
	}
}

func TestFailuresComeBackAsCategories(t *testing.T) {
	for _, tc := range []struct {
		status int
		ctype  string
		body   string
		want   assistant.Category
	}{
		{401, "application/json", `{"error":{"message":"Incorrect API key"}}`, assistant.ErrAuth},
		{403, "application/json", `{}`, assistant.ErrAuth},
		{404, "application/json", `{"error":{"message":"model not found"}}`, assistant.ErrNotFound},
		{429, "application/json", `{"error":{"type":"rate_limit_error"}}`, assistant.ErrRateLimited},
		{429, "application/json", `{"error":{"code":"insufficient_quota"}}`, assistant.ErrQuotaExhausted},
		{402, "application/json", `{"error":{"message":"Insufficient Balance"}}`, assistant.ErrQuotaExhausted},
		{400, "application/json", `{"error":{"message":"tools not supported"}}`, assistant.ErrInvalidRequest},
		{503, "text/html", `<html>down</html>`, assistant.ErrUnavailable},
		{200, "application/json", `{"choices":[{"message":{"content":"not streamed"}}]}`, assistant.ErrBadResponse},
		{200, "text/event-stream", "data: [DONE]\n\n", assistant.ErrBadResponse},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.ctype)
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, _, err := collect(t, openai.New(assistant.Endpoint{Format: assistant.FormatOpenAI, BaseURL: srv.URL, Key: "k"}), hello)
		srv.Close()
		if got := assistant.CategoryOf(err); got != tc.want {
			t.Errorf("%d %s: category = %q (%v), want %q", tc.status, tc.body, got, err, tc.want)
		}
	}
}

func TestErrorInsideStream(t *testing.T) {
	rp := newReplay(t, "stream_error.sse")
	events, _, err := collect(t, rp.adapter(), hello)
	if got := assistant.CategoryOf(err); got != assistant.ErrRateLimited {
		t.Errorf("category = %q (%v)", got, err)
	}
	if text(events, assistant.EventText) != "半" {
		t.Errorf("text before the error should still have streamed")
	}
}

func TestUnreachableEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, _, err := collect(t, openai.New(assistant.Endpoint{Format: assistant.FormatOpenAI, BaseURL: url, Key: "k"}), hello)
	if got := assistant.CategoryOf(err); got != assistant.ErrUnavailable {
		t.Errorf("category = %q (%v)", got, err)
	}
}

func TestTruncatedToolCallReportsLength(t *testing.T) {
	rp := newReplay(t, "truncated_call.sse")
	_, res, err := collect(t, rp.adapter(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if res.Finish != assistant.FinishLength {
		t.Errorf("finish = %q, want length: the call was cut off", res.Finish)
	}
}

// history is a run's messages after one call that thought and called a tool.
var history = []assistant.Message{
	{Role: assistant.RoleUser, Content: "Raft 里的任期是什么？"},
	{Role: assistant.RoleAssistant, Reasoning: "先查一下课程。", ToolCalls: []assistant.ToolCall{{ID: "call_1", Name: "search_course", Arguments: `{"keyword":"任期"}`}}},
	{Role: assistant.RoleTool, ToolCallID: "call_1", Content: "第 5 讲：任期是……"},
}

func TestDeepSeekStreamsReasoningApartAndMapsItsUsage(t *testing.T) {
	rp := newReplay(t, "deepseek_thinking.sse")
	events, res, err := collect(t, rp.deepseek(), assistant.Request{Model: "deepseek-flash", Messages: history, Thinking: true})
	if err != nil {
		t.Fatal(err)
	}
	if text(events, assistant.EventReasoning) == "" || text(events, assistant.EventText) == "" {
		t.Fatalf("expected reasoning and text apart, got %+v", events)
	}
	u := res.Usage
	if u.Input == nil || *u.Input != 237 || u.CacheRead == nil || *u.CacheRead != 768 || u.Output == nil || *u.Output != 152 || u.Reasoning == nil || *u.Reasoning != 112 {
		t.Fatalf("usage: input 237 (cache misses), cache read 768, output 152 of which reasoning 112; got %s", show(u))
	}
	if u.CacheWrite != nil {
		t.Fatalf("DeepSeek reports no cache writes, got %d", *u.CacheWrite)
	}
}

func TestDeepSeekSwitchesThinkingAndPassesReasoningBack(t *testing.T) {
	for _, on := range []bool{true, false} {
		rp := newReplay(t, "deepseek_thinking.sse")
		if _, _, err := collect(t, rp.deepseek(), assistant.Request{Model: "deepseek-flash", Messages: history, Thinking: on}); err != nil {
			t.Fatal(err)
		}
		want := map[bool]string{true: "enabled", false: "disabled"}[on]
		if th, _ := rp.body["thinking"].(map[string]any); th["type"] != want {
			t.Fatalf("thinking %v should send type %q, got %v", on, want, rp.body["thinking"])
		}
		msgs := rp.body["messages"].([]any)
		if got := msgs[1].(map[string]any)["reasoning_content"]; got != "先查一下课程。" {
			t.Fatalf("the reasoning of the run's earlier call goes back as it was, got %v", got)
		}
	}
}

func TestAPlainEndpointGetsNoProviderFields(t *testing.T) {
	rp := newReplay(t, "text.sse")
	if _, _, err := collect(t, rp.adapter(), assistant.Request{Model: "m", Messages: history, Thinking: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := rp.body["thinking"]; ok {
		t.Fatalf("a plain endpoint gets no thinking switch: %v", rp.body)
	}
	if _, ok := rp.body["messages"].([]any)[1].(map[string]any)["reasoning_content"]; ok {
		t.Fatalf("a plain endpoint gets no reasoning back: %v", rp.body["messages"])
	}
}

func show(u assistant.Usage) string {
	b, _ := json.Marshal(u)
	return string(b)
}

// cutOff is a request continuing a reply cut off at the output cap.
var cutOff = append(append([]assistant.Message(nil), history...), assistant.Message{Role: assistant.RoleAssistant, Content: "[{\"name\": \"极", Prefix: true})

func TestDeepSeekContinuesACutOffReplyAsWritten(t *testing.T) {
	rp := newReplay(t, "text.sse")
	if _, _, err := collect(t, rp.official(), assistant.Request{Model: "deepseek-chat", Messages: cutOff}); err != nil {
		t.Fatal(err)
	}
	if rp.path != "/beta/chat/completions" {
		t.Fatalf("prefix completion is DeepSeek's beta, got %s", rp.path)
	}
	msgs := rp.body["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if len(msgs) != len(cutOff) || last["role"] != "assistant" || last["prefix"] != true || last["content"] != cutOff[len(cutOff)-1].Content {
		t.Fatalf("the cut-off reply goes last, as a prefix: %v", msgs)
	}
}

func TestOtherEndpointsAreAskedToContinue(t *testing.T) {
	for name, c := range map[string]struct {
		adapter  func(*replay) *openai.Adapter
		thinking bool
	}{
		"plain endpoint":         {(*replay).adapter, false},
		"DeepSeek elsewhere":     {(*replay).deepseek, false},
		"DeepSeek with thinking": {(*replay).official, true},
	} {
		rp := newReplay(t, "text.sse")
		if _, _, err := collect(t, c.adapter(rp), assistant.Request{Model: "m", Messages: cutOff, Thinking: c.thinking}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rp.path, "/beta/") {
			t.Errorf("%s: sent to %s", name, rp.path)
		}
		msgs := rp.body["messages"].([]any)
		cut, ask := msgs[len(msgs)-2].(map[string]any), msgs[len(msgs)-1].(map[string]any)
		if _, ok := cut["prefix"]; ok || cut["role"] != "assistant" || cut["content"] != cutOff[len(cutOff)-1].Content {
			t.Errorf("%s: the cut-off reply goes back as an assistant message, got %v", name, cut)
		}
		if ask["role"] != "user" || !strings.Contains(ask["content"].(string), "接着写") {
			t.Errorf("%s: then the model is asked to go on, got %v", name, ask)
		}
	}
}
