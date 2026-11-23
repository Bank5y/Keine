package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
	"github.com/Bank5y/keine/internal/testutil"
)

// withOwnModels opens readers' own models (#139), letting plain http and
// private addresses through within the given networks.
func withOwnModels(networks ...string) appOption {
	return func(o *server.Options) { o.OwnModels, o.OwnModelNetworks = true, networks }
}

const ownPath = "/api/me/own-model"

func TestOwnModelsAreClosedUnlessTheDeployerOpensThem(t *testing.T) {
	t.Parallel()
	a := newApp(t, withMasterKey(masterKey))
	bob := a.user("bob")
	for _, r := range []resp{
		bob.do("GET", ownPath, nil),
		bob.do("PUT", ownPath, map[string]any{"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-own-0123456789", "model": "m"}),
		bob.do("PUT", ownPath+"/use", map[string]any{"on": true}),
		bob.do("DELETE", ownPath, nil),
	} {
		expectError(t, r, http.StatusNotFound, "own_models_disabled")
	}
	if own, _ := me(t, bob)["ownModels"].(map[string]any); own == nil || own["enabled"] != false {
		t.Fatalf("/api/me ownModels = %v", me(t, bob)["ownModels"])
	}
}

func TestOwnModelsCannotBeOpenedWithoutAMasterKey(t *testing.T) {
	t.Parallel()
	_, err := server.New(context.Background(), server.Options{DatabaseURL: testutil.NewDatabase(t), OwnModels: true})
	if err == nil {
		t.Fatal("started with own models open and no master key")
	}
}

func ownBody(baseURL, key string) map[string]any {
	return map[string]any{"format": "openai", "baseUrl": baseURL, "key": key, "model": "my-model", "contextWindow": 64000}
}

func TestAReaderSavesTheirOwnModelAfterAConnectionTest(t *testing.T) {
	t.Parallel()
	a := newApp(t, withMasterKey(masterKey), withOwnModels("127.0.0.0/8"))
	f := newFakeModel(t)
	bob := a.user("bob")
	expectError(t, bob.do("GET", ownPath, nil), http.StatusNotFound, "own_model_not_found")

	r := bob.do("PUT", ownPath, ownBody(f.url(), "sk-own-secret-w9x8"))
	expectStatus(t, r, http.StatusOK)
	m := r.json()
	if m["keyLast4"] != "w9x8" || m["model"] != "my-model" || m["search"] != "off" || m["inUse"] != false || m["contextWindow"] != float64(64000) {
		t.Fatalf("saved own model: %s", r.body)
	}
	if strings.Contains(string(r.body), "sk-own-secret") || strings.Contains(string(bob.do("GET", ownPath, nil).body), "sk-own-secret") {
		t.Fatalf("the key came back: %s", r.body)
	}
	// the test was one streaming call declaring a tool, with the reader's key,
	// and asking for no parallel tool calls of an endpoint of unknown make
	if f.calls() != 1 || f.lastKey() != "sk-own-secret-w9x8" {
		t.Fatalf("connection test: %d calls, key %q", f.calls(), f.lastKey())
	}
	if req := f.requests[0]; req["stream"] != true || len(req["tools"].([]any)) == 0 || req["parallel_tool_calls"] != false {
		t.Fatalf("connection test request: %v", req)
	}

	var stored []byte
	conn, err := pgx.Connect(context.Background(), a.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if err := conn.QueryRow(context.Background(), "SELECT api_key FROM own_models").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 || strings.Contains(string(stored), "sk-own-secret") {
		t.Fatalf("key not stored encrypted: %q", stored)
	}

	// the admin sees nothing of it
	admin := a.admin()
	users := admin.mustOK(admin.do("GET", "/api/admin/users", nil))
	if strings.Contains(string(users.body), "my-model") || strings.Contains(string(users.body), "w9x8") {
		t.Fatalf("the admin's user list shows the own model: %s", users.body)
	}

	// a failed test saves nothing
	f.fail = http.StatusUnauthorized
	expectError(t, bob.do("PUT", ownPath, ownBody(f.url(), "sk-own-other-1111")), http.StatusUnprocessableEntity, "model_test_failed")
	if got := bob.mustOK(bob.do("GET", ownPath, nil)).json(); got["keyLast4"] != "w9x8" {
		t.Fatalf("a failed test changed the model: %v", got)
	}
}

// blockedAt tries to save an own model at baseURL and expects the guard to
// have stopped it before it reached anything.
func blockedAt(t *testing.T, c *client, baseURL string) {
	t.Helper()
	r := c.do("PUT", ownPath, ownBody(baseURL, "sk-own-0123456789"))
	expectError(t, r, http.StatusUnprocessableEntity, "model_test_failed")
	if cat := r.json()["error"].(map[string]any)["category"]; cat != "blocked_address" {
		t.Fatalf("%s: category %v: %s", baseURL, cat, r.body)
	}
}

func TestAnOwnModelCannotReachInsideAddresses(t *testing.T) {
	t.Parallel()
	f := newFakeModel(t)
	port := f.srv.URL[strings.LastIndex(f.srv.URL, ":"):]
	a := newApp(t, withMasterKey(masterKey), withOwnModels("169.254.0.0/16", "10.9.0.0/16"))
	bob := a.user("bob")
	for _, u := range []string{
		"https://127.0.0.1" + port + "/v1", // loopback
		"http://127.0.0.1" + port + "/v1",
		"https://localhost" + port + "/v1", // a name that resolves inside
		"https://10.0.0.1/v1",              // private, outside the deployer's networks
		"https://192.168.1.10/v1",
		"http://169.254.169.254/latest/meta-data", // metadata, though inside the networks
		"https://[::1]" + port + "/v1",
		"http://[::ffff:127.0.0.1]" + port + "/v1",
	} {
		blockedAt(t, bob, u)
	}
	if f.calls() != 0 {
		t.Fatalf("the model was reached %d times", f.calls())
	}
	// plain http to a public address is not let through either
	blockedAt(t, bob, "http://93.184.215.14/v1")
}

func TestAnOwnModelInTheDeployersNetworksIsReachedOverHTTPButNotRedirected(t *testing.T) {
	t.Parallel()
	f := newFakeModel(t)
	var redirected int
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++ }))
	t.Cleanup(elsewhere.Close)
	mover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(mover.Close)
	a := newApp(t, withMasterKey(masterKey), withOwnModels("127.0.0.0/8"))
	bob := a.user("bob")

	bob.mustOK(bob.do("PUT", ownPath, ownBody(f.url(), "sk-own-0123456789")))
	if f.calls() != 1 {
		t.Fatalf("the model in the deployer's network was not reached: %d calls", f.calls())
	}
	r := bob.do("PUT", ownPath, ownBody(mover.URL+"/v1", "sk-own-0123456789"))
	expectError(t, r, http.StatusUnprocessableEntity, "model_test_failed")
	if redirected != 0 {
		t.Fatal("the redirect was followed")
	}
}

const ownURL = "https://own.example/v1"

// withSiteAndOwn runs the site's models on site and every reader's own
// model (at ownURL) on own.
func withSiteAndOwn(site, own *scriptedModel) appOption {
	return func(o *server.Options) {
		o.AdapterFor = func(e assistant.Endpoint) assistant.Adapter {
			if e.BaseURL == ownURL {
				return own
			}
			return site
		}
	}
}

// ownApp is an app with the site's assistant mapped to site and own
// models open, and a reader bob running on his own model, searching as
// search says (with the Tavily key for "tavily").
func ownApp(t *testing.T, site, own *scriptedModel, f *fakeSearch, search string) (*app, *client) {
	t.Helper()
	a := newApp(t, withSiteAndOwn(site, own), withMasterKey(masterKey), withOwnModels(), withSearch(f))
	mapModel(t, a, map[string]any{"searchRoute": "tavily"}, false)
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/tavily", map[string]any{"key": "tvly-site-key-1234"}))
	bob := a.user("bob")
	body := ownBody(ownURL, "sk-own-0123456789")
	body["search"], body["tavilyKey"] = search, "tvly-bobs-key-5678"
	bob.mustOK(bob.do("PUT", ownPath, body))
	if m := bob.mustOK(bob.do("PUT", ownPath+"/use", map[string]any{"on": true})).json(); m["inUse"] != true {
		t.Fatalf("switch: %v", m)
	}
	return a, bob
}

func TestARunOnTheReadersOwnModelCostsNothingAndIsRecordedAsTheirs(t *testing.T) {
	t.Parallel()
	site := newScriptedModel()
	reply := texts("任期就是 term。")
	reply.result.Usage = assistant.Usage{Input: n64(1_000_000), Output: n64(500_000)}
	own := newScriptedModel(costly(), reply)
	a, bob := ownApp(t, site, own, newFakeSearch(), "off")
	// no credits at all: an own model needs none
	onPlan(t, a, bob, 0)
	id, lesson := analysisColumn(t, bob)

	events, r := analyze(t, bob, id, lesson, agentQuote)
	expectStatus(t, r, http.StatusOK)
	done := lastEvent(t, events)
	if done.name != "done" {
		t.Fatalf("analysis on the own model: %+v", done)
	}
	exp := idOf(pin(bob, id, lesson, done.data["analysisId"].(string)).json())
	ask(t, bob, exp, firstTurn(t, bob, exp), "任期是什么？")

	if len(site.calls()) != 0 || len(own.calls()) != 2 {
		t.Fatalf("site model %d calls, own %d", len(site.calls()), len(own.calls()))
	}
	if own.calls()[0].Model != "my-model" {
		t.Fatalf("own model called as %q", own.calls()[0].Model)
	}
	if runs := runsOf(t, a, bob); len(runs) != 0 {
		t.Fatalf("own model runs left usage rows: %v", runs)
	}
	if cr := credits(t, bob); !near(cr["remaining"], 0) || !near(cr["used"], 0) {
		t.Fatalf("credits = %v", cr)
	}
	for _, m := range turnMetas(t, a, exp) {
		if m["source"] != "own" || m["model"] != "my-model" {
			t.Fatalf("turn meta = %v", m)
		}
	}
}

func TestAnOwnModelFailingIsTheReadersModelErrorAndNeverFallsBackToTheSites(t *testing.T) {
	t.Parallel()
	site := newScriptedModel(texts(goodAnswer))
	own := newScriptedModel(step{err: &assistant.Error{Category: assistant.ErrAuth, Detail: "bad key"}})
	a, bob := ownApp(t, site, own, newFakeSearch(), "off")
	id, lesson := analysisColumn(t, bob)

	events, _ := analyze(t, bob, id, lesson, agentQuote)
	last := lastEvent(t, events)
	if last.name != "error" || last.data["code"] != "own_model_error" || last.data["category"] != "auth" {
		t.Fatalf("expected own_model_error, got %+v", last)
	}
	if len(site.calls()) != 0 {
		t.Fatal("the site's model was called")
	}
	if runs := runsOf(t, a, bob); len(runs) != 0 {
		t.Fatalf("usage rows: %v", runs)
	}
}

func TestAnOwnModelOfflineDeclaresNoSearchAndTheReaderIsTold(t *testing.T) {
	t.Parallel()
	own := newScriptedModel(texts(goodAnswer))
	f := newFakeSearch()
	_, bob := ownApp(t, newScriptedModel(), own, f, "off")
	id, lesson := analysisColumn(t, bob)
	analyze(t, bob, id, lesson, agentQuote)

	if got := declared(own.calls()[0]); has(got, "web_search") || has(got, "web_fetch") || own.calls()[0].ProviderWeb {
		t.Fatalf("offline own model declared %v", got)
	}
	st := me(t, bob)["ownModels"].(map[string]any)
	if st["inUse"] != true || st["offline"] != true || me(t, bob)["aiSearch"] != "" {
		t.Fatalf("/api/me = %v", me(t, bob))
	}
}

func TestAnOwnModelSearchesOnTheReadersTavilyKeyNeverTheSites(t *testing.T) {
	t.Parallel()
	own := newScriptedModel(texts(goodAnswer))
	f := newFakeSearch()
	_, bob := ownApp(t, newScriptedModel(), own, f, "tavily")
	id, lesson := analysisColumn(t, bob)
	analyze(t, bob, id, lesson, agentQuote)

	if got := declared(own.calls()[0]); !has(got, "web_search") {
		t.Fatalf("own model with Tavily declared %v", got)
	}
	f.mu.Lock()
	routes := append([]string(nil), f.routes...)
	f.mu.Unlock()
	// the test search when saving, then the run's route
	if !has(routes, "tavily:tvly-bobs-key-5678") {
		t.Fatalf("routes built: %v", routes)
	}
	for _, r := range routes[1:] {
		if r == "tavily:tvly-site-key-1234" {
			t.Fatalf("the reader's run used the site's Tavily key: %v", routes)
		}
	}
}

func TestDeletingTheOwnModelTurnsItOff(t *testing.T) {
	t.Parallel()
	site := newScriptedModel(texts(goodAnswer))
	own := newScriptedModel()
	_, bob := ownApp(t, site, own, newFakeSearch(), "off")

	expectStatus(t, bob.do("DELETE", ownPath, nil), http.StatusNoContent)
	expectError(t, bob.do("GET", ownPath, nil), http.StatusNotFound, "own_model_not_found")
	if st := me(t, bob)["ownModels"].(map[string]any); st["inUse"] != false {
		t.Fatalf("still in use: %v", st)
	}
	expectError(t, bob.do("PUT", ownPath+"/use", map[string]any{"on": true}), http.StatusNotFound, "own_model_not_found")

	id, lesson := analysisColumn(t, bob)
	analyze(t, bob, id, lesson, agentQuote)
	if len(site.calls()) != 1 || len(own.calls()) != 0 {
		t.Fatalf("after deleting: site %d calls, own %d", len(site.calls()), len(own.calls()))
	}
}

func TestAnOwnKeyTheMasterKeyNoLongerOpensAsksTheReaderToEnterItAgain(t *testing.T) {
	t.Parallel()
	site, own := newScriptedModel(texts(goodAnswer)), newScriptedModel()
	a, bob := ownApp(t, site, own, newFakeSearch(), "off")
	id, lesson := analysisColumn(t, bob)
	b := a.restart(withSiteAndOwn(site, own), withMasterKey("another-master-key-0123456789abcdef"), withOwnModels(), withSearch(newFakeSearch()))
	bob = b.as("bob", "bob-password")

	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusConflict, "own_key_unreadable")
	if len(site.calls()) != 0 {
		t.Fatal("the site's model was called")
	}
}

func TestATavilyKeyNoLongerSearchedWithIsNotKept(t *testing.T) {
	t.Parallel()
	_, bob := ownApp(t, newScriptedModel(), newScriptedModel(), newFakeSearch(), "tavily")
	if m := bob.mustOK(bob.do("GET", ownPath, nil)).json(); m["tavilyLast4"] != "5678" {
		t.Fatalf("Tavily key not kept while searched with: %v", m)
	}
	body := ownBody(ownURL, "")
	body["search"] = "off"
	if m := bob.mustOK(bob.do("PUT", ownPath, body)).json(); m["tavilyLast4"] != "" || m["keyLast4"] != "6789" {
		t.Fatalf("after switching search off: %v", m)
	}
	// back to Tavily, a key has to be given again
	body["search"] = "tavily"
	expectError(t, bob.do("PUT", ownPath, body), http.StatusBadRequest, "tavily_key_empty")
}
