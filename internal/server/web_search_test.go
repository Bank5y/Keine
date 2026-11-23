package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

// fakeSearch is the search service every web route resolves to. Each
// search and fetch is kept; what they answer is up to search and fetch
// (by default: two hits for any query, a short page for any URL).
type fakeSearch struct {
	mu       sync.Mutex
	queries  []string
	fetched  []string
	routes   []string
	search   func(n int, q string) ([]assistant.SearchHit, bool, error)
	fetch    func(url string) (assistant.Page, bool, error)
	searches int
}

func newFakeSearch() *fakeSearch {
	return &fakeSearch{
		search: func(_ int, q string) ([]assistant.SearchHit, bool, error) {
			return []assistant.SearchHit{
				{Title: "Raft 论文", URL: "https://raft.github.io/raft.pdf", Summary: "Raft 把共识拆成领导选举、日志复制和安全性。"},
				{Title: "任期", URL: "https://example.com/term", Summary: strings.Repeat("任", 800)},
			}, true, nil
		},
		fetch: func(url string) (assistant.Page, bool, error) {
			return assistant.Page{Title: "Raft 论文", Content: "Raft 用任期（term）给时间编号。"}, true, nil
		},
	}
}

func (f *fakeSearch) Search(_ context.Context, q string, count int) ([]assistant.SearchHit, bool, error) {
	f.mu.Lock()
	f.searches++
	n := f.searches
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	return f.search(n, q)
}

func (f *fakeSearch) Fetch(_ context.Context, url string) (assistant.Page, bool, error) {
	f.mu.Lock()
	f.fetched = append(f.fetched, url)
	f.mu.Unlock()
	return f.fetch(url)
}

func (f *fakeSearch) requests() (queries, fetched []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...), append([]string(nil), f.fetched...)
}

// withSearch puts f behind every web route: DeepSeek searches and fetches,
// Tavily only searches. Each route built is noted with its key.
func withSearch(f *fakeSearch) appOption {
	return func(o *server.Options) {
		o.SearchFor = func(route assistant.SearchRoute, key string) assistant.WebRoute {
			f.mu.Lock()
			f.routes = append(f.routes, string(route)+":"+key)
			f.mu.Unlock()
			w := assistant.WebRoute{Search: f}
			if route == assistant.RouteDeepSeek {
				w.Fetch = f
			}
			return w
		}
	}
}

// deepseekApp is an app whose assistant runs on m as a DeepSeek model
// with the search route left to resolve, which gives DeepSeek Search /
// Fetch on the model's key.
func deepseekApp(t *testing.T, m *scriptedModel, f *fakeSearch) *app {
	t.Helper()
	a := newApp(t, withModel(m), withMasterKey(masterKey), withSearch(f))
	mapModel(t, a, map[string]any{"provider": "deepseek", "baseUrl": ""}, false)
	return a
}

func declared(req assistant.Request) []string {
	var out []string
	for _, s := range req.Tools {
		out = append(out, s.Name)
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sourcesOf(t *testing.T, done sseEvent) []map[string]any {
	t.Helper()
	raw, ok := done.data["sources"].([]any)
	if !ok {
		t.Fatalf("done has no sources: %v", done.data)
	}
	var out []map[string]any
	for _, s := range raw {
		out = append(out, s.(map[string]any))
	}
	return out
}

func TestASearchAndAFetchGoIntoTheTurnsSources(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	f.fetch = func(string) (assistant.Page, bool, error) {
		return assistant.Page{Title: "Raft 论文", Content: strings.Repeat("x", 25_000)}, true, nil
	}
	search := call("web_search", map[string]any{"query": "Raft 任期"})
	fetch := call("web_fetch", map[string]any{"url": "https://raft.github.io/raft.pdf"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{search}), toolStep([]assistant.ToolCall{fetch}), texts(goodAnswer))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	done := lastEvent(t, events)
	if done.name != "done" {
		t.Fatalf("last event = %+v", done)
	}
	calls := m.calls()
	if d := declared(calls[0]); !has(d, "web_search") || !has(d, "web_fetch") {
		t.Fatalf("declared tools = %v", d)
	}
	if f.routes[len(f.routes)-1] != "deepseek:sk-test-0123456789" {
		t.Errorf("routes built = %v, want DeepSeek on the model's key", f.routes)
	}

	results := toolResult(t, calls[1], search)
	if !strings.Contains(results, "Raft 论文") || !strings.Contains(results, "https://example.com/term") {
		t.Errorf("search result = %q", results)
	}
	if strings.Contains(results, strings.Repeat("任", 501)) || !strings.Contains(results, strings.Repeat("任", 500)) {
		t.Error("a hit's summary is not cut to 500 characters")
	}
	page := toolResult(t, calls[2], fetch)
	if n := strings.Count(page, "x"); n != 20_000 || !strings.Contains(page, "已截断") {
		t.Errorf("page has %d characters of text, marked %v", n, strings.Contains(page, "已截断"))
	}

	want := []map[string]any{
		{"title": "Raft 论文", "url": "https://raft.github.io/raft.pdf", "kind": "search"},
		{"title": "任期", "url": "https://example.com/term", "kind": "search"},
		{"title": "Raft 论文", "url": "https://raft.github.io/raft.pdf", "kind": "fetch"},
	}
	got := sourcesOf(t, done)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
	if _, ok := done.data["degraded"]; ok {
		t.Errorf("a run that searched is degraded: %v", done.data["degraded"])
	}
	acts := activities(events)
	if len(acts) != 4 || acts[0]["kind"] != "web_search" || acts[0]["target"] != "Raft 任期" || acts[3]["target"] != "raft.github.io" {
		t.Errorf("activities = %v", acts)
	}
}

func TestFetchingAURLNotSearchedIsAWrongCall(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	fetch := call("web_fetch", map[string]any{"url": "https://elsewhere.example/"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{fetch}), texts(goodAnswer))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if r := toolResult(t, m.calls()[1], fetch); !strings.Contains(r, "只能抓取 web_search 返回过的 URL") {
		t.Errorf("fetch result = %q", r)
	}
	if _, fetched := f.requests(); len(fetched) != 0 {
		t.Errorf("fetched %v", fetched)
	}
	acts := activities(events)
	if acts[len(acts)-1]["status"] != "failed" || acts[len(acts)-1]["reason"] != "invalid_request" {
		t.Errorf("activities = %v", acts)
	}
	// a wrong call is not the web going off
	if _, ok := lastEvent(t, events).data["degraded"]; ok {
		t.Error("a wrong call degraded the run")
	}
}

func TestTwoServerErrorsInARowTurnSearchOff(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	f.search = func(int, string) ([]assistant.SearchHit, bool, error) {
		return nil, false, assistant.Transient(assistant.ErrUnavailable, fmt.Errorf("503"))
	}
	c1 := call("web_search", map[string]any{"query": "a"})
	c2 := call("web_search", map[string]any{"query": "b"})
	c3 := call("web_search", map[string]any{"query": "c"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{c1}), toolStep([]assistant.ToolCall{c2}), toolStep([]assistant.ToolCall{c3}), texts(goodAnswer))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	queries, _ := f.requests()
	// each failing call is tried once more; the third sends nothing
	if fmt.Sprint(queries) != "[a a b b]" {
		t.Errorf("searches sent = %v", queries)
	}
	calls := m.calls()
	if r := toolResult(t, calls[3], c3); !strings.Contains(r, "本次运行联网搜索不可用") || !strings.Contains(r, "不要编造来源") {
		t.Errorf("third result = %q", r)
	}
	if d := declared(calls[3]); !has(d, "web_search") {
		t.Errorf("web_search left the declaration: %v", d)
	}
	done := lastEvent(t, events)
	if done.name != "done" || fmt.Sprint(done.data["degraded"]) != "[web_search]" {
		t.Fatalf("done = %v", done.data)
	}
	if len(sourcesOf(t, done)) != 0 {
		t.Error("failed searches left sources")
	}
	// the short-circuited call is not shown
	if n := len(activities(events)); n != 4 {
		t.Errorf("%d activities, want 2 calls running and failing", n)
	}
}

func TestNoBalanceTurnsSearchOffAtOnceButKeepsFetchingWhatWasFound(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	ok := f.search
	f.search = func(n int, q string) ([]assistant.SearchHit, bool, error) {
		if n == 1 {
			return ok(n, q)
		}
		return nil, false, assistant.AccountFailure(assistant.ErrQuotaExhausted, fmt.Errorf("402"))
	}
	s1 := call("web_search", map[string]any{"query": "a"})
	s2 := call("web_search", map[string]any{"query": "b"})
	s3 := call("web_search", map[string]any{"query": "c"})
	fetch := call("web_fetch", map[string]any{"url": "https://example.com/term"})
	m := newScriptedModel(toolStep([]assistant.ToolCall{s1}), toolStep([]assistant.ToolCall{s2}), toolStep([]assistant.ToolCall{s3, fetch}), texts(goodAnswer))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	queries, fetched := f.requests()
	if fmt.Sprint(queries) != "[a b]" || fmt.Sprint(fetched) != "[https://example.com/term]" {
		t.Errorf("searches %v, fetches %v", queries, fetched)
	}
	var failed map[string]any
	for _, act := range activities(events) {
		if act["status"] == "failed" {
			failed = act
		}
	}
	if failed["reason"] != "quota_exhausted" || failed["ownKey"] != nil {
		t.Errorf("failed activity = %v", failed)
	}
	done := lastEvent(t, events)
	if fmt.Sprint(done.data["degraded"]) != "[web_search]" {
		t.Errorf("degraded = %v", done.data["degraded"])
	}
	if n := len(sourcesOf(t, done)); n != 3 {
		t.Errorf("%d sources, want the two hits and the page", n)
	}
}

func TestTheTavilyRouteSearchesWithTheSitesKeyAndFetchesNothing(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	m := newScriptedModel(texts(goodAnswer))
	a := newApp(t, withModel(m), withMasterKey(masterKey), withSearch(f))
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/tavily", map[string]any{"key": "tvly-site-key-1234"}))
	mapModel(t, a, map[string]any{}, false)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	if d := declared(m.calls()[0]); !has(d, "web_search") || has(d, "web_fetch") {
		t.Errorf("declared tools = %v", d)
	}
	if f.routes[len(f.routes)-1] != "tavily:tvly-site-key-1234" {
		t.Errorf("routes built = %v", f.routes)
	}
}

func TestNoWebIsTheDeployersChoice(t *testing.T) {
	t.Parallel()
	m := newScriptedModel(texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	if d := declared(m.calls()[0]); has(d, "web_search") || has(d, "web_fetch") {
		t.Errorf("declared tools = %v", d)
	}
	if done := lastEvent(t, events); done.name != "done" || len(sourcesOf(t, done)) != 0 {
		t.Errorf("done = %v", done.data)
	}
}

func TestAIUnavailableUntilEveryModelHasASearchRoute(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey), withSearch(f))
	admin := a.admin()
	alice := a.user("alice")

	// a plain model with the route left to resolve needs the site's Tavily key
	mapModel(t, a, map[string]any{}, false)
	st := listModels(admin)
	model := st["models"].([]any)[0].(map[string]any)
	if aiAvailable(t, alice) || st["aiAvailable"] != false || model["searchReady"] != false {
		t.Fatalf("AI available without a search route: %v", st)
	}
	id, lesson := analysisColumn(t, alice)
	_, r := analyze(t, alice, id, lesson, agentQuote)
	expectError(t, r, http.StatusConflict, "ai_unavailable")

	// the key is tested with one search before it is kept
	f.search = func(int, string) ([]assistant.SearchHit, bool, error) {
		return nil, false, assistant.AccountFailure(assistant.ErrAuth, fmt.Errorf("401 tvly-bad-key-9999"))
	}
	r = admin.do("PUT", "/api/admin/tavily", map[string]any{"key": "tvly-bad-key-9999"})
	expectError(t, r, http.StatusUnprocessableEntity, "tavily_test_failed")
	if strings.Contains(string(r.body), "tvly-bad") || aiAvailable(t, alice) {
		t.Fatalf("a failed key: %s", r.body)
	}

	f.search = newFakeSearch().search
	st = admin.mustOK(admin.do("PUT", "/api/admin/tavily", map[string]any{"key": "tvly-good-key-5678"})).json()
	model = st["models"].([]any)[0].(map[string]any)
	if !aiAvailable(t, alice) || model["search"] != "tavily" || model["searchReady"] != true {
		t.Fatalf("AI not available with a Tavily key: %v", st)
	}
	if tv := st["tavily"].(map[string]any); tv["set"] != true || tv["keyLast4"] != "5678" {
		t.Errorf("tavily = %v", tv)
	}
	if strings.Contains(fmt.Sprint(st), "tvly-good") {
		t.Error("the key is shown")
	}

	admin.mustOK(admin.do("DELETE", "/api/admin/tavily", nil))
	if aiAvailable(t, alice) {
		t.Fatal("AI still available after the Tavily key was removed")
	}
	expectError(t, alice.do("PUT", "/api/admin/tavily", map[string]any{"key": "x"}), http.StatusForbidden, "forbidden")
}

func TestDeepSeekSearchIsOnlyForADeepSeekModel(t *testing.T) {
	t.Parallel()
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey), withSearch(newFakeSearch()))
	admin := a.admin()
	r := admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-1", "model": "m", "displayName": "M", "rates": rates, "searchRoute": "deepseek",
	})
	expectError(t, r, http.StatusBadRequest, "invalid_model")
	r = admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-1", "model": "m", "displayName": "M", "rates": rates, "searchRoute": "google",
	})
	expectError(t, r, http.StatusBadRequest, "invalid_model")
}
