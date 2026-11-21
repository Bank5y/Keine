package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestAnUpgradedSiteKeepsTheAssistant starts from a site that mapped the
// three kinds before reports and the concept index existed: the report and
// the concept index take the model and thinking switch sync had (ADR 0012).
func TestAnUpgradedSiteKeepsTheAssistant(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	body := modelBody(f, "sk-1")
	body["searchRoute"] = "off"
	flash := addModel(t, admin, body)
	body["displayName"] = "Pro"
	pro := addModel(t, admin, body)
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": flash}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/followup", map[string]any{"modelId": flash}))
	rollBackRunKinds(t, a.dbURL, fmt.Sprintf(`INSERT INTO turn_models (kind, model_id, thinking) VALUES ('sync', %d, true)`, pro))

	b := a.restart(withMasterKey(masterKey))
	admin = b.admin()

	turns := listModels(admin)["turns"].(map[string]any)
	for _, k := range []string{"report", "concept_index"} {
		if m, _ := turns[k].(map[string]any); m["modelId"] != float64(pro) || m["thinking"] != true {
			t.Fatalf("%s takes sync's model and thinking: %v", k, turns)
		}
	}
	if _, ok := turns["sync"]; ok {
		t.Fatalf("sync is still a kind: %v", turns)
	}
	if !aiAvailable(t, b.user("alice")) {
		t.Fatalf("the assistant is gone after the upgrade: %v", modelsProblem(t, admin))
	}
}

func TestANewSiteNeedsTheReportAndTheConceptIndexMapped(t *testing.T) {
	a := newApp(t, withMasterKey(masterKey))
	f := newFakeModel(t)
	admin := a.admin()
	body := modelBody(f, "sk-1")
	body["searchRoute"] = "off"
	id := addModel(t, admin, body)
	admin.mustOK(admin.do("PUT", "/api/admin/turns/analysis", map[string]any{"modelId": id}))
	admin.mustOK(admin.do("PUT", "/api/admin/turns/followup", map[string]any{"modelId": id}))

	if aiAvailable(t, a.user("alice")) {
		t.Fatal("AI available with the report and the concept index unmapped")
	}
	if p := aiProblem(t, admin); p["reason"] != "turns_unmapped" || fmt.Sprint(p["turns"]) != "[report concept_index]" {
		t.Fatalf("the missing kinds: %v", p)
	}
	expectError(t, admin.do("PUT", "/api/admin/turns/sync", map[string]any{"modelId": id}), http.StatusNotFound, "not_found")
}

// rollBackRunKinds turns the database back into its shape before the run
// kinds migration, when sync was a kind, then runs setup on it.
func rollBackRunKinds(t *testing.T, dbURL, setup string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{
		`DELETE FROM turn_models WHERE kind IN ('report', 'concept_index')`,
		`ALTER TABLE turn_models DROP CONSTRAINT turn_models_kind_check`,
		`ALTER TABLE turn_models ADD CONSTRAINT turn_models_kind_check CHECK (kind IN ('analysis', 'followup', 'sync'))`,
		`DELETE FROM schema_migrations WHERE name = '0026_run_kinds.sql'`,
		setup,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}
