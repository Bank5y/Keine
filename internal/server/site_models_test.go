package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

const masterKey = "test-master-key-0123456789abcdef0123"

func withMasterKey(k string) appOption {
	return func(o *server.Options) { o.MasterKey = k }
}

// fakeModel is an OpenAI-compatible endpoint: it answers with a short
// stream, or with the status set in fail, and keeps what it was sent.
type fakeModel struct {
	srv *httptest.Server
	mu  sync.Mutex
	// fail is the status to answer with instead of a stream; 0 streams.
	fail     int
	requests []map[string]any
	keys     []string
}

func newFakeModel(t *testing.T) *fakeModel {
	f := &fakeModel{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		f.keys = append(f.keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		fail := f.fail
		f.mu.Unlock()
		if fail != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(fail)
			_, _ = io.WriteString(w, `{"error":{"message":"upstream says: Incorrect API key sk-secret"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModel) url() string { return f.srv.URL + "/v1" }

func (f *fakeModel) lastKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keys) == 0 {
		return ""
	}
	return f.keys[len(f.keys)-1]
}

func (f *fakeModel) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

var rates = map[string]any{"input": 2, "cacheRead": 0.2, "cacheWrite": 2, "output": 8, "search": 1}

func modelBody(f *fakeModel, key string) map[string]any {
	return map[string]any{"format": "openai", "baseUrl": f.url(), "key": key, "model": "m-1", "displayName": "Flash", "rates": rates}
}

func addModel(t *testing.T, admin *client, body map[string]any) int {
	t.Helper()
	m := admin.mustOK(admin.do("POST", "/api/admin/models", body)).json()
	return int(m["id"].(float64))
}

func listModels(admin *client) map[string]any {
	return admin.mustOK(admin.do("GET", "/api/admin/models", nil)).json()
}

func aiAvailable(t *testing.T, c *client) bool {
	t.Helper()
	v, ok := c.mustOK(c.do("GET", "/api/me", nil)).json()["aiAvailable"].(bool)
	if !ok {
		t.Fatal("/api/me has no aiAvailable")
	}
	return v
}

func TestAdminAddsSiteModel(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()

	r := admin.do("POST", "/api/admin/models", modelBody(f, "sk-secret-a1b2"))
	expectStatus(t, r, http.StatusCreated)
	m := r.json()
	if m["keyLast4"] != "a1b2" || m["displayName"] != "Flash" || m["model"] != "m-1" || m["format"] != "openai" || m["contextWindow"] != nil {
		t.Fatalf("unexpected model: %s", r.body)
	}
	if strings.Contains(string(r.body), "sk-secret") {
		t.Fatalf("response leaks the key: %s", r.body)
	}

	// the connection test was one streaming call with a tool, made with the key
	if f.calls() != 1 || f.lastKey() != "sk-secret-a1b2" {
		t.Fatalf("connection test: %d calls, key %q", f.calls(), f.lastKey())
	}
	req := f.requests[0]
	if req["stream"] != true || req["model"] != "m-1" || len(req["tools"].([]any)) == 0 {
		t.Fatalf("connection test request: %v", req)
	}

	list := listModels(admin)
	models := list["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["keyLast4"] != "a1b2" || list["masterKey"] != true {
		t.Fatalf("unexpected list: %v", list)
	}
	if got := models[0].(map[string]any)["rates"].(map[string]any)["cacheRead"]; got != 0.2 {
		t.Fatalf("rates = %v", models[0])
	}

	// stored encrypted
	conn, err := pgx.Connect(context.Background(), a.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var stored []byte
	if err := conn.QueryRow(context.Background(), "SELECT api_key FROM site_models").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 || strings.Contains(string(stored), "sk-secret") {
		t.Fatalf("key not stored encrypted: %q", stored)
	}

	// a key so short its last 4 would give half of it away shows nothing
	if m := admin.mustOK(admin.do("POST", "/api/admin/models", modelBody(f, "sk-ab12"))).json(); m["keyLast4"] != "" {
		t.Fatalf("short key shown: %v", m)
	}
}

func TestOnlyAdminsConfigureModels(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	id := addModel(t, a.admin(), modelBody(f, "sk-1"))
	alice := a.user("alice")

	for _, r := range []resp{
		alice.do("GET", "/api/admin/models", nil),
		alice.do("POST", "/api/admin/models", modelBody(f, "sk-2")),
		alice.do("PUT", fmt.Sprintf("/api/admin/models/%d", id), modelBody(f, "sk-2")),
		alice.do("DELETE", fmt.Sprintf("/api/admin/models/%d", id), nil),
		alice.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": id, "thinking": false}),
	} {
		expectError(t, r, http.StatusForbidden, "forbidden")
	}
	if f.calls() != 1 {
		t.Fatalf("a refused request reached the model: %d calls", f.calls())
	}
}

func TestKeyNeedsMasterKey(t *testing.T) {
	a := newApp(t)
	f := newFakeModel(t)
	admin := a.admin()

	expectError(t, admin.do("POST", "/api/admin/models", modelBody(f, "sk-1")), http.StatusBadRequest, "master_key_missing")
	if f.calls() != 0 {
		t.Fatal("tested a model that cannot be saved")
	}
	// an endpoint without auth needs no key
	addModel(t, admin, modelBody(f, ""))
	list := listModels(admin)
	if list["masterKey"] != false || list["models"].([]any)[0].(map[string]any)["keyLast4"] != "" {
		t.Fatalf("unexpected list: %v", list)
	}
}

func TestFailedConnectionTestSavesNothing(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	id := addModel(t, admin, modelBody(f, "sk-good"))

	f.fail = http.StatusUnauthorized
	r := admin.do("POST", "/api/admin/models", modelBody(f, "sk-bad"))
	expectError(t, r, http.StatusUnprocessableEntity, "model_test_failed")
	if r.json()["error"].(map[string]any)["category"] != "auth" {
		t.Fatalf("no category: %s", r.body)
	}
	if strings.Contains(string(r.body), "upstream") || strings.Contains(string(r.body), "sk-") {
		t.Fatalf("provider response leaked: %s", r.body)
	}

	edit := modelBody(f, "sk-bad")
	edit["displayName"] = "Renamed"
	expectError(t, admin.do("PUT", fmt.Sprintf("/api/admin/models/%d", id), edit), http.StatusUnprocessableEntity, "model_test_failed")

	models := listModels(admin)["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["displayName"] != "Flash" {
		t.Fatalf("a failed test changed the models: %v", models)
	}

	f.fail = http.StatusServiceUnavailable
	r = admin.do("POST", "/api/admin/models", modelBody(f, "sk-x"))
	if r.json()["error"].(map[string]any)["category"] != "unavailable" {
		t.Fatalf("category: %s", r.body)
	}
}

func TestEditKeepsKeyWhenLeftBlank(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	id := addModel(t, admin, modelBody(f, "sk-first-a1b2"))

	// after a redeploy with the same master key the stored key still works
	a = a.restart(withMasterKey(masterKey))
	admin = a.admin()
	edit := modelBody(f, "")
	edit["displayName"] = "Renamed"
	edit["contextWindow"] = 64000
	m := admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/models/%d", id), edit)).json()
	if f.lastKey() != "sk-first-a1b2" || m["keyLast4"] != "a1b2" || m["displayName"] != "Renamed" || m["contextWindow"] != float64(64000) {
		t.Fatalf("edit: key %q, model %v", f.lastKey(), m)
	}

	m = admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/models/%d", id), modelBody(f, "sk-second-9f0c"))).json()
	if f.lastKey() != "sk-second-9f0c" || m["keyLast4"] != "9f0c" {
		t.Fatalf("new key: %q, model %v", f.lastKey(), m)
	}
}

func TestInvalidModelsAreRejected(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	for name, change := range map[string]func(map[string]any){
		"format":        func(m map[string]any) { m["format"] = "anthropic" },
		"base url":      func(m map[string]any) { m["baseUrl"] = "ftp://example.com" },
		"model":         func(m map[string]any) { m["model"] = " " },
		"display name":  func(m map[string]any) { m["displayName"] = "" },
		"context":       func(m map[string]any) { m["contextWindow"] = 0 },
		"partial rates": func(m map[string]any) { m["rates"] = map[string]any{"input": 1, "output": 2} },
		"negative rate": func(m map[string]any) {
			m["rates"] = map[string]any{"input": -1, "cacheRead": 0, "cacheWrite": 0, "output": 2, "search": 0}
		},
	} {
		body := modelBody(f, "sk-1")
		change(body)
		expectError(t, admin.do("POST", "/api/admin/models", body), http.StatusBadRequest, "invalid_model")
		_ = name
	}
	if f.calls() != 0 {
		t.Fatal("tested an invalid model")
	}
}

func TestUnratedModelCannotBeMapped(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	unrated := modelBody(f, "sk-1")
	unrated["rates"] = nil
	id := addModel(t, admin, unrated)
	if listModels(admin)["models"].([]any)[0].(map[string]any)["rates"] != nil {
		t.Fatal("an unrated model shows rates")
	}

	expectError(t, admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": id}), http.StatusConflict, "model_unrated")

	rated := addModel(t, admin, modelBody(f, "sk-2"))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": rated, "thinking": true}))
	// a mapped model keeps its rates
	expectError(t, admin.do("PUT", fmt.Sprintf("/api/admin/models/%d", rated), unrated), http.StatusConflict, "model_unrated")
}

func TestAIAvailableOnceEveryTurnKindIsMapped(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	alice := a.user("alice")
	id := addModel(t, admin, modelBody(f, "sk-1"))

	if aiAvailable(t, alice) || aiAvailable(t, admin) {
		t.Fatal("AI available without a mapping")
	}
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": id}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/followup", map[string]any{"modelId": id}))
	if aiAvailable(t, alice) {
		t.Fatal("AI available with sync unmapped")
	}
	st := admin.mustOK(admin.do("PUT", "/api/admin/turns/sync", map[string]any{"modelId": id, "thinking": true})).json()
	if !aiAvailable(t, alice) || st["aiAvailable"] != true {
		t.Fatalf("AI not available with every kind mapped: %v", st)
	}
	turns := listModels(admin)["turns"].(map[string]any)
	if s := turns["sync"].(map[string]any); s["modelId"] != float64(id) || s["thinking"] != true {
		t.Fatalf("turns = %v", turns)
	}

	expectError(t, admin.do("DELETE", fmt.Sprintf("/api/admin/models/%d", id), nil), http.StatusConflict, "model_in_use")

	admin.mustOK(admin.do("PUT", "/api/admin/turns/sync", map[string]any{"modelId": nil}))
	if aiAvailable(t, alice) {
		t.Fatal("AI still available after unmapping sync")
	}
	if listModels(admin)["turns"].(map[string]any)["sync"] != nil {
		t.Fatal("sync still mapped")
	}
	expectError(t, admin.do("PUT", "/api/admin/turns/summary", map[string]any{"modelId": id}), http.StatusNotFound, "not_found")
	expectError(t, admin.do("PUT", "/api/admin/turns/sync", map[string]any{"modelId": 99999}), http.StatusNotFound, "model_not_found")
}

func TestAdminDeletesModel(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	id := addModel(t, admin, modelBody(f, "sk-1"))

	expectStatus(t, admin.do("DELETE", fmt.Sprintf("/api/admin/models/%d", id), nil), http.StatusNoContent)
	if len(listModels(admin)["models"].([]any)) != 0 {
		t.Fatal("model still listed")
	}
	expectError(t, admin.do("DELETE", fmt.Sprintf("/api/admin/models/%d", id), nil), http.StatusNotFound, "model_not_found")
	expectError(t, admin.do("PUT", fmt.Sprintf("/api/admin/models/%d", id), modelBody(f, "sk-1")), http.StatusNotFound, "model_not_found")
}

func TestChoosingDeepSeekFillsInItsAddressAndName(t *testing.T) {
	m := newScriptedModel()
	var endpoints []assistant.Endpoint
	var mu sync.Mutex
	a := newApp(t, withMasterKey(masterKey), func(o *server.Options) {
		o.AdapterFor = func(e assistant.Endpoint) assistant.Adapter {
			mu.Lock()
			endpoints = append(endpoints, e)
			mu.Unlock()
			return m
		}
	})
	admin := a.admin()

	state := admin.mustOK(admin.do("GET", "/api/admin/models", nil))
	presets := state.list("providers")
	if len(presets) != 1 {
		t.Fatalf("expected the DeepSeek preset, got %v", presets)
	}
	ds := presets[0].(map[string]any)
	if ds["id"] != "deepseek" || ds["baseUrl"] != "https://api.deepseek.com" || ds["displayName"] != "DeepSeek" {
		t.Fatalf("unexpected preset %v", ds)
	}

	created := admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "provider": "deepseek", "key": "sk-deepseek-0123456789", "model": "deepseek-flash", "rates": rates,
	})).json()
	if created["provider"] != "deepseek" || created["baseUrl"] != "https://api.deepseek.com" || created["displayName"] != "DeepSeek" {
		t.Fatalf("only key and model name were given, the rest comes from the preset: %v", created)
	}
	mu.Lock()
	last := endpoints[len(endpoints)-1]
	mu.Unlock()
	if last.Provider != assistant.ProviderDeepSeek || last.BaseURL != "https://api.deepseek.com" || last.Key != "sk-deepseek-0123456789" {
		t.Fatalf("the connection test should run with the provider configuration, got %+v", last)
	}

	r := admin.do("POST", "/api/admin/models", map[string]any{"format": "openai", "provider": "kimi", "model": "x", "baseUrl": "https://x.example", "displayName": "X"})
	expectError(t, r, http.StatusBadRequest, "invalid_model")
}
