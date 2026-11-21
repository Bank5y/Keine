package server_test

import (
	"fmt"
	"testing"
)

// aiProblem is why /api/me says the assistant is unavailable, nil when it
// is available.
func aiProblem(t *testing.T, c *client) map[string]any {
	t.Helper()
	got := me(t, c)
	p, _ := got["aiProblem"].(map[string]any)
	if (p == nil) != (got["aiAvailable"] == true) {
		t.Fatalf("aiAvailable %v with aiProblem %v", got["aiAvailable"], got["aiProblem"])
	}
	return p
}

func TestTheReaderIsToldWhyTheAssistantIsUnavailable(t *testing.T) {
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey))
	bob := a.user("bob")
	if p := aiProblem(t, bob); p["reason"] != "turns_unmapped" || fmt.Sprint(p["turns"]) != "[analysis followup sync]" {
		t.Fatalf("with no model: %v", p)
	}

	admin := a.admin()
	m := admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-test-0123456789", "model": "m-1", "displayName": "Qwen", "rates": rates,
	})).json()
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": m["id"]}))
	if p := aiProblem(t, bob); p["reason"] != "turns_unmapped" || fmt.Sprint(p["turns"]) != "[followup sync]" {
		t.Fatalf("with analysis mapped: %v", p)
	}

	admin.mustOK(admin.do("PUT", "/api/admin/turns/followup", map[string]any{"modelId": m["id"]}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/sync", map[string]any{"modelId": m["id"]}))
	// searching on auto, with neither a search of its own nor a Tavily key
	if p := aiProblem(t, bob); p["reason"] != "search_unrouted" || p["model"] != "Qwen" {
		t.Fatalf("with no search route: %v", p)
	}

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/models/%v", m["id"]), map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "model": "m-1", "displayName": "Qwen", "rates": rates, "searchRoute": "off",
	}))
	if p := aiProblem(t, bob); p != nil {
		t.Fatalf("with no web chosen, the assistant is available: %v", p)
	}
}
