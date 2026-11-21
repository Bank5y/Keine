package server_test

import (
	"fmt"
	"testing"
)

// aiProblem is why /api/me says the assistant is unavailable, nil when it
// is available or the reader is not the admin.
func aiProblem(t *testing.T, c *client) map[string]any {
	t.Helper()
	p, _ := me(t, c)["aiProblem"].(map[string]any)
	return p
}

// modelsProblem is why the admin's models page says the assistant is off.
func modelsProblem(t *testing.T, admin *client) map[string]any {
	t.Helper()
	p, _ := admin.mustOK(admin.do("GET", "/api/admin/models", nil)).json()["problem"].(map[string]any)
	return p
}

func TestTheAdminIsToldWhyTheAssistantIsUnavailable(t *testing.T) {
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey))
	admin := a.admin()
	if p := aiProblem(t, admin); p["reason"] != "turns_unmapped" || fmt.Sprint(p["turns"]) != "[analysis followup report concept_index]" {
		t.Fatalf("with no model: %v", p)
	}

	newModel := func(name string) any {
		return admin.mustOK(admin.do("POST", "/api/admin/models", map[string]any{
			"format": "openai", "baseUrl": "https://model.example/v1", "key": "sk-test-0123456789", "model": "m-1", "displayName": name, "rates": rates,
		})).json()["id"]
	}
	qwen, glm := newModel("Qwen"), newModel("GLM")
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": qwen}))
	if p := aiProblem(t, admin); p["reason"] != "turns_unmapped" || fmt.Sprint(p["turns"]) != "[followup report concept_index]" {
		t.Fatalf("with analysis mapped: %v", p)
	}

	admin.mustOK(admin.do("PUT", "/api/admin/turns/followup", map[string]any{"modelId": glm}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/report", map[string]any{"modelId": qwen}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/concept_index", map[string]any{"modelId": qwen}))
	// both search on auto, with neither a search of their own nor a Tavily key
	p := aiProblem(t, admin)
	if p["reason"] != "search_unrouted" || fmt.Sprint(p["models"]) != "[GLM Qwen]" {
		t.Fatalf("with no search route: %v", p)
	}
	if fmt.Sprint(modelsProblem(t, admin)) != fmt.Sprint(p) {
		t.Fatalf("the models page says %v, /api/me %v", modelsProblem(t, admin), p)
	}

	for _, m := range []struct {
		id   any
		name string
	}{{qwen, "Qwen"}, {glm, "GLM"}} {
		admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/models/%v", m.id), map[string]any{
			"format": "openai", "baseUrl": "https://model.example/v1", "model": "m-1", "displayName": m.name, "rates": rates, "searchRoute": "off",
		}))
	}
	if got := me(t, admin); got["aiAvailable"] != true || got["aiProblem"] != nil {
		t.Fatalf("with no web chosen, the assistant is available: %v", got)
	}
	if p := modelsProblem(t, admin); p != nil {
		t.Fatalf("the models page still has a problem: %v", p)
	}
}

func TestAReaderIsNotToldTheDetails(t *testing.T) {
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey))
	got := me(t, a.user("bob"))
	if got["aiAvailable"] != false || got["aiProblem"] != nil {
		t.Fatalf("a reader's /api/me: aiAvailable %v, aiProblem %v", got["aiAvailable"], got["aiProblem"])
	}
}
