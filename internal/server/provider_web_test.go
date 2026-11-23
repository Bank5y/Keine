package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
)

// anthropicApp is an app whose assistant runs on m as an Anthropic model
// with the search route left to resolve, which gives the provider's own
// search and fetch. f is behind the Keine tool route, which it must not
// take.
func anthropicApp(t *testing.T, m *scriptedModel, f *fakeSearch) *app {
	t.Helper()
	a := newApp(t, withModel(m), withMasterKey(masterKey), withSearch(f))
	mapModel(t, a, map[string]any{"format": "anthropic", "baseUrl": "", "model": "claude-opus-5-5", "displayName": "Claude"}, false)
	return a
}

// webStep is an event of the provider's own search.
func webStep(id, kind, target string, status assistant.ActivityStatus, reason assistant.Category, sources ...assistant.WebSource) assistant.Event {
	return assistant.Event{Kind: assistant.EventWeb, Web: &assistant.WebStep{ID: id, Kind: kind, Target: target, Status: status, Reason: reason}, Sources: sources}
}

var (
	raftPaper = assistant.WebSource{Title: "Raft 论文", URL: "https://raft.github.io/raft.pdf", Kind: assistant.WebSourceSearch}
	raftHome  = assistant.WebSource{Title: "Raft 主页", URL: "https://raft.github.io/", Kind: assistant.WebSourceSearch}
)

func TestAnAnthropicModelSearchesWithTheProvidersOwnTools(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	answer := texts(goodAnswer)
	answer.events = append([]assistant.Event{
		webStep("srv_1", "web_search", "Raft 任期", assistant.ActivityRunning, ""),
		webStep("srv_1", "web_search", "Raft 任期", assistant.ActivityDone, "", raftPaper),
		webStep("srv_2", "web_fetch", "raft.github.io", assistant.ActivityRunning, ""),
		webStep("srv_2", "web_fetch", "raft.github.io", assistant.ActivityDone, "", assistant.WebSource{Title: "Raft 论文", URL: "https://raft.github.io/raft.pdf", Kind: assistant.WebSourceFetch}),
		// a citation of a result already found adds nothing, even under
		// another title
		{Kind: assistant.EventSources, Sources: []assistant.WebSource{{Title: "In Search of Raft", URL: raftPaper.URL, Kind: assistant.WebSourceSearch}, raftHome}},
	}, answer.events...)
	m := newScriptedModel(answer)
	a := anthropicApp(t, m, f)
	admin := a.admin()
	bob := a.user("bob")

	// no Tavily key is needed
	model := listModels(admin)["models"].([]any)[0].(map[string]any)
	if model["search"] != "provider" || model["searchReady"] != true || model["baseUrl"] != "https://api.anthropic.com" {
		t.Fatalf("model = %v", model)
	}
	if !aiAvailable(t, bob) {
		t.Fatal("AI unavailable")
	}

	id, lesson := analysisColumn(t, bob)
	events, _ := analyze(t, bob, id, lesson, agentQuote)
	done := lastEvent(t, events)
	if done.name != "done" {
		t.Fatalf("last event = %+v", done)
	}
	req := m.calls()[0]
	if !req.ProviderWeb {
		t.Error("the provider's search was not declared")
	}
	if d := declared(req); has(d, "web_search") || has(d, "web_fetch") {
		t.Errorf("Keine's web tools declared too: %v", d)
	}
	if q, _ := f.requests(); len(q) != 0 {
		t.Errorf("the Keine tool route searched: %v", q)
	}

	var got []string
	for _, act := range activities(events) {
		got = append(got, act["id"].(string)+" "+act["kind"].(string)+" "+act["target"].(string)+" "+act["status"].(string))
	}
	want := []string{
		"web-srv_1 web_search Raft 任期 running",
		"web-srv_1 web_search Raft 任期 done",
		"web-srv_2 web_fetch raft.github.io running",
		"web-srv_2 web_fetch raft.github.io done",
	}
	if jsonOf(got) != jsonOf(want) {
		t.Errorf("activities = %s", jsonOf(got))
	}

	var sources []string
	for _, s := range sourcesOf(t, done) {
		sources = append(sources, s["kind"].(string)+" "+s["url"].(string))
	}
	wantSources := []string{"search https://raft.github.io/raft.pdf", "fetch https://raft.github.io/raft.pdf", "search https://raft.github.io/"}
	if jsonOf(sources) != jsonOf(wantSources) {
		t.Errorf("sources = %s", jsonOf(sources))
	}
	if done.data["degraded"] != nil {
		t.Errorf("degraded = %v", done.data["degraded"])
	}
}

func TestAProvidersSearchErrorIsAFailedActivityAndTurnsNothingOff(t *testing.T) {
	t.Parallel()
	first := toolStep([]assistant.ToolCall{call("list_sections", map[string]any{"lesson": "l01"})})
	first.events = append([]assistant.Event{
		webStep("srv_1", "web_search", "Raft", assistant.ActivityRunning, ""),
		webStep("srv_1", "web_search", "Raft", assistant.ActivityFailed, assistant.ErrRateLimited),
	}, first.events...)
	second := texts(goodAnswer)
	second.events = append([]assistant.Event{
		webStep("srv_2", "web_search", "Raft", assistant.ActivityRunning, ""),
		webStep("srv_2", "web_search", "Raft", assistant.ActivityFailed, assistant.ErrUnavailable),
	}, second.events...)
	m := newScriptedModel(first, second)
	a := anthropicApp(t, m, newFakeSearch())
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	done := lastEvent(t, events)
	if done.name != "done" {
		t.Fatalf("last event = %+v", done)
	}
	var failed []string
	for _, act := range activities(events) {
		if act["status"] == "failed" {
			failed = append(failed, act["id"].(string)+" "+act["reason"].(string))
		}
	}
	if jsonOf(failed) != `["web-srv_1 rate_limited","web-srv_2 unavailable"]` {
		t.Errorf("failed = %s", jsonOf(failed))
	}
	// the provider's search stays declared, and no capability is off
	if calls := m.calls(); len(calls) != 2 || !calls[1].ProviderWeb {
		t.Errorf("calls = %d", len(calls))
	}
	if done.data["degraded"] != nil {
		t.Errorf("degraded = %v", done.data["degraded"])
	}
}

func TestTheRunPassesTheProvidersTurnBackAsItWas(t *testing.T) {
	t.Parallel()
	lookup := call("list_sections", map[string]any{"lesson": "l01"})
	first := toolStep([]assistant.ToolCall{lookup})
	first.result.Native = "turn 1 as the provider wrote it"
	m := newScriptedModel(first, texts(goodAnswer))
	a := anthropicApp(t, m, newFakeSearch())
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	if done := lastEvent(t, func() []sseEvent { e, _ := analyze(t, bob, id, lesson, agentQuote); return e }()); done.name != "done" {
		t.Fatalf("last event = %+v", done)
	}
	var passed []any
	for _, msg := range m.calls()[1].Messages {
		if msg.Role == assistant.RoleAssistant {
			passed = append(passed, msg.Native)
		}
	}
	if len(passed) != 1 || passed[0] != "turn 1 as the provider wrote it" {
		t.Errorf("passed back = %v", passed)
	}
}

func TestTheProvidersSearchIsOnlyForAFormatThatHasIt(t *testing.T) {
	t.Parallel()
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey), withSearch(newFakeSearch()))
	admin := a.admin()
	r := admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-1", "model": "m", "displayName": "M", "rates": rates, "searchRoute": "provider",
	})
	expectError(t, r, http.StatusBadRequest, "invalid_model")
	r = admin.do("POST", "/api/admin/models", map[string]any{
		"format": "anthropic", "provider": "deepseek", "key": "sk-1", "model": "m", "displayName": "M", "rates": rates,
	})
	expectError(t, r, http.StatusBadRequest, "invalid_model")
	// the deployer may still take an Anthropic model off the web, or to Tavily
	for _, route := range []string{"off", "tavily"} {
		admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
			"format": "anthropic", "key": "sk-ant-1", "model": "claude-opus-5-5", "displayName": "Claude", "rates": rates, "searchRoute": route,
		}))
	}
}

func TestTheConnectionTestOfAProviderRoutedModelDeclaresItsSearch(t *testing.T) {
	t.Parallel()
	m := &recordingTests{scriptedModel: newScriptedModel()}
	a := newApp(t, withModel(m), withMasterKey(masterKey))
	admin := a.admin()
	admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
		"format": "anthropic", "key": "sk-ant-1", "model": "claude-opus-5-5", "displayName": "Claude", "rates": rates,
	}))
	admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
		"format": "anthropic", "key": "sk-ant-1", "model": "claude-opus-5-5", "displayName": "Claude", "rates": rates, "searchRoute": "off",
	}))
	if len(m.tests) != 2 || !m.tests[0].ProviderWeb || m.tests[1].ProviderWeb {
		t.Errorf("connection tests = %+v", m.tests)
	}
}

// recordingTests is a scripted model that keeps the connection tests.
type recordingTests struct {
	*scriptedModel
	tests []assistant.Request
}

func (m *recordingTests) Stream(ctx context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	if len(req.Tools) == 1 && req.Tools[0].Name == "connection_check" {
		m.tests = append(m.tests, req)
	}
	return m.scriptedModel.Stream(ctx, req, emit)
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
