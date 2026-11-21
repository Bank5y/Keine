package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/server"
)

func n64(n int64) *int64 { return &n }

// costly is an answer whose call used a million input tokens, a million
// cache hits and half a million output tokens: at the test rates (2, 0.2
// and 8 credits a million) it costs 6.2 credits.
func costly() step {
	s := texts(goodAnswer)
	s.result.Usage = assistant.Usage{Input: n64(1_000_000), CacheRead: n64(1_000_000), CacheWrite: n64(0), Output: n64(500_000), Reasoning: n64(100_000)}
	return s
}

// onPlan puts the user on a new plan of daily credits (nil: unlimited)
// and returns the plan.
func onPlan(t *testing.T, a *app, user *client, daily any) float64 {
	t.Helper()
	admin := a.admin()
	plan := admin.mustOK(admin.do("POST", "/api/admin/plans", map[string]any{"name": fmt.Sprintf("套餐 %v %d", daily, time.Now().UnixNano()), "daily": daily})).json()
	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%v", me(t, user)["id"]), map[string]any{"planId": plan["id"]}))
	return plan["id"].(float64)
}

// credits is what /api/me says of the reader's credits.
func credits(t *testing.T, c *client) map[string]any {
	t.Helper()
	cr, _ := me(t, c)["credits"].(map[string]any)
	if cr == nil {
		t.Fatal("/api/me has no credits")
	}
	return cr
}

// usageOf is what the admin's usage page says of one reader.
func usageOf(t *testing.T, a *app, user *client) map[string]any {
	t.Helper()
	admin := a.admin()
	return admin.mustOK(admin.do("GET", fmt.Sprintf("/api/admin/usage/users/%v", me(t, user)["id"]), nil)).json()
}

func runsOf(t *testing.T, a *app, user *client) []any {
	t.Helper()
	return usageOf(t, a, user)["runs"].([]any)
}

func near(a any, b float64) bool {
	f, ok := a.(float64)
	return ok && f-b < 1e-9 && b-f < 1e-9
}

func TestARunIsChargedAtTheRatesOfItsTime(t *testing.T) {
	m := newScriptedModel(costly())
	a := aiApp(t, m)
	bob := a.user("bob")
	onPlan(t, a, bob, 100)
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	runs := runsOf(t, a, bob)
	if len(runs) != 1 {
		t.Fatalf("one run should leave one usage row, got %d", len(runs))
	}
	run := runs[0].(map[string]any)
	if run["kind"] != "analysis" || run["model"] != "Flash" || run["estimated"] != false ||
		run["input"] != 1e6 || run["cacheRead"] != 1e6 || run["output"] != 5e5 || run["reasoning"] != 1e5 || !near(run["credits"], 6.2) {
		t.Fatalf("usage row = %v", run)
	}
	if cr := credits(t, bob); cr["daily"] != 100.0 || !near(cr["remaining"], 93.8) {
		t.Fatalf("credits = %v", cr)
	}

	// the rates go up: what was charged stays
	admin := a.admin()
	models := admin.mustOK(admin.do("GET", "/api/admin/models", nil)).list("models")
	mid := models[0].(map[string]any)["id"]
	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/models/%v", mid), map[string]any{
		"format": "openai", "baseUrl": "https://model.example/v1", "model": "m-1", "displayName": "Flash", "searchRoute": "off",
		"rates": map[string]any{"input": 20, "cacheRead": 2, "cacheWrite": 20, "output": 80, "search": 10},
	}))
	if run := runsOf(t, a, bob)[0].(map[string]any); !near(run["credits"], 6.2) {
		t.Fatalf("after the rates changed, the row says %v", run["credits"])
	}
}

func TestARunThatMayCostMoreThanIsLeftDoesNotStart(t *testing.T) {
	m := newScriptedModel()
	a := aiApp(t, m)
	bob := a.user("bob")
	onPlan(t, a, bob, 1)
	id, lesson := analysisColumn(t, bob)

	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusPaymentRequired, "credits_insufficient")
	cr, _ := r.json()["error"].(map[string]any)["credits"].(map[string]any)
	// two searches at 1 credit each are more than the 1 left on their own
	if est, _ := cr["estimate"].(float64); est <= 2 || cr["remaining"] != 1.0 {
		t.Fatalf("credits = %v", cr)
	}
	if len(m.calls()) != 0 {
		t.Fatal("the model was called")
	}
}

func TestARunMayOverdrawButTheNextWaitsForTheResetAtMidnight(t *testing.T) {
	m := newScriptedModel(costly(), texts(goodAnswer))
	y, mo, d := time.Now().Date()
	clock := &fakeClock{now: time.Date(y, mo, d, 23, 0, 0, 0, time.Local)}
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob := a.user("bob")
	onPlan(t, a, bob, 3)
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	if cr := credits(t, bob); !near(cr["remaining"], -3.2) {
		t.Fatalf("the run is charged in full: %v", cr)
	}
	_, r := analyze(t, bob, id, lesson, agentQuote)
	expectError(t, r, http.StatusPaymentRequired, "credits_insufficient")

	clock.advance(time.Hour) // midnight
	if cr := credits(t, bob); cr["remaining"] != 3.0 {
		t.Fatalf("after midnight, nothing carries over: %v", cr)
	}
	if events, _ := analyze(t, bob, id, lesson, agentQuote); lastEvent(t, events).name != "done" {
		t.Fatalf("after midnight the reader can analyze again: %+v", lastEvent(t, events))
	}
}

func TestChangingThePlanTakesEffectAtOnceAndUnlimitedIsStillMetered(t *testing.T) {
	m := newScriptedModel(costly(), costly())
	a := aiApp(t, m)
	bob := a.user("bob")
	onPlan(t, a, bob, 10)
	id, lesson := analysisColumn(t, bob)

	analyze(t, bob, id, lesson, agentQuote)
	onPlan(t, a, bob, 20)
	if cr := credits(t, bob); cr["daily"] != 20.0 || !near(cr["remaining"], 13.8) {
		t.Fatalf("after the plan changed: %v", cr)
	}

	onPlan(t, a, bob, nil)
	if cr := credits(t, bob); cr["daily"] != nil || cr["remaining"] != nil {
		t.Fatalf("on an unlimited plan: %v", cr)
	}
	if events, _ := analyze(t, bob, id, lesson, agentQuote); lastEvent(t, events).name != "done" {
		t.Fatalf("an unlimited plan is not held back: %+v", lastEvent(t, events))
	}
	if n := len(runsOf(t, a, bob)); n != 2 {
		t.Fatalf("an unlimited plan is still metered: %d rows", n)
	}
}

func TestTheAdminStartsUnlimitedAndNewReadersOnTheDefaultPlan(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	if cr := credits(t, admin); cr["daily"] != nil || cr["plan"] != "不限" {
		t.Fatalf("the admin's credits = %v", cr)
	}
	plan := admin.mustOK(admin.do("POST", "/api/admin/plans", map[string]any{"name": "免费", "daily": 0})).json()
	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/plans/%v/default", plan["id"]), nil))
	if cr := credits(t, a.user("bob")); cr["daily"] != 0.0 || cr["plan"] != "免费" {
		t.Fatalf("a new reader's credits = %v", cr)
	}
}

func TestAReaderMadeTheAdminMovesToUnlimited(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	boss := a.user("boss")
	if cr := credits(t, boss); cr["plan"] != "基础" {
		t.Fatalf("a reader starts on the default plan: %v", cr)
	}
	// the deployment names an existing account as its admin
	a = a.restart(withModel(newScriptedModel()), withMasterKey(masterKey), withAdmin("boss", "boss-password"))
	if cr := credits(t, a.as("boss", "boss-password")); cr["plan"] != "不限" || cr["daily"] != nil {
		t.Fatalf("once made the admin: %v", cr)
	}
}

func TestARunCutShortIsChargedByEstimate(t *testing.T) {
	s := texts("<exp_title>Agent</exp_title>")
	s.hold = true
	m := newScriptedModel(s)
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	b, _ := json.Marshal(agentQuote)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", a.srv.URL+analysisPath(id, lesson), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := bob.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() && !strings.HasPrefix(sc.Text(), "data: ") {
	}
	cancel()
	res.Body.Close()

	var runs []any
	for deadline := time.Now().Add(5 * time.Second); len(runs) == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		runs = runsOf(t, a, bob)
	}
	if len(runs) != 1 {
		t.Fatalf("the cut-short run left %d rows", len(runs))
	}
	if run := runs[0].(map[string]any); run["estimated"] != true || run["input"].(float64) <= 0 || run["output"].(float64) <= 0 {
		t.Fatalf("usage row = %v", run)
	}
}

func TestPlansAreTheAdminsToManage(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	bob := a.user("bob")

	plan := admin.mustOK(admin.do("POST", "/api/admin/plans", map[string]any{"name": "进阶", "daily": 200})).json()
	pid := plan["id"]
	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/plans/%v", pid), map[string]any{"name": "进阶版", "daily": 300}))
	expectError(t, admin.do("POST", "/api/admin/plans", map[string]any{"name": "进阶版", "daily": 1}), http.StatusConflict, "plan_name_taken")
	expectError(t, admin.do("POST", "/api/admin/plans", map[string]any{"name": "负数", "daily": -1}), http.StatusBadRequest, "invalid_plan")

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%v", me(t, bob)["id"]), map[string]any{"planId": pid}))
	expectError(t, admin.do("DELETE", fmt.Sprintf("/api/admin/plans/%v", pid), nil), http.StatusConflict, "plan_in_use")

	var unlimited, def any
	for _, p := range admin.mustOK(admin.do("GET", "/api/admin/plans", nil)).list("plans") {
		p := p.(map[string]any)
		switch {
		case p["name"] == "进阶版":
			if p["daily"] != 300.0 || p["users"] != 1.0 {
				t.Errorf("plan = %v", p)
			}
		case p["builtin"] == true:
			unlimited = p["id"]
		case p["default"] == true:
			def = p["id"]
		}
	}
	expectError(t, admin.do("DELETE", fmt.Sprintf("/api/admin/plans/%v", unlimited), nil), http.StatusConflict, "plan_builtin")
	expectError(t, admin.do("DELETE", fmt.Sprintf("/api/admin/plans/%v", def), nil), http.StatusConflict, "plan_default")

	admin.mustOK(admin.do("PUT", fmt.Sprintf("/api/admin/users/%v", me(t, bob)["id"]), map[string]any{"planId": def}))
	expectStatus(t, admin.do("DELETE", fmt.Sprintf("/api/admin/plans/%v", pid), nil), http.StatusNoContent)
}

func TestTheAdminsUsersListShowsEachPlanAndWhatWasUsedToday(t *testing.T) {
	m := newScriptedModel(costly())
	a := aiApp(t, m)
	bob := a.user("bob")
	onPlan(t, a, bob, 20)
	id, lesson := analysisColumn(t, bob)
	analyze(t, bob, id, lesson, agentQuote)

	admin := a.admin()
	for _, u := range admin.mustOK(admin.do("GET", "/api/admin/users", nil)).list("users") {
		u := u.(map[string]any)
		if u["username"] != "bob" {
			continue
		}
		cr := u["credits"].(map[string]any)
		if cr["daily"] != 20.0 || !near(cr["used"], 6.2) || cr["planId"] == nil {
			t.Fatalf("bob's credits = %v", cr)
		}
		return
	}
	t.Fatal("bob is not listed")
}

func TestUsageAddsUpByDayAndByReaderOverTheChosenDays(t *testing.T) {
	m := newScriptedModel(costly(), costly(), costly())
	clock := &fakeClock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)}
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob, alice := a.user("bob"), a.user("alice")
	onPlan(t, a, bob, nil)
	onPlan(t, a, alice, nil)
	id, lesson := analysisColumn(t, bob)
	hers, herLesson := analysisColumn(t, alice)

	analyze(t, bob, id, lesson, agentQuote)
	clock.advance(24 * time.Hour)
	analyze(t, bob, id, lesson, agentQuote)
	analyze(t, alice, hers, herLesson, agentQuote)

	admin := a.admin()
	all := admin.mustOK(admin.do("GET", "/api/admin/usage?from=2026-11-20&to=2026-11-21", nil)).json()
	total := all["total"].(map[string]any)
	if total["runs"] != 3.0 || !near(total["credits"], 18.6) || total["input"] != 3e6 {
		t.Fatalf("total = %v", total)
	}
	days := all["days"].([]any)
	if len(days) != 2 || days[0].(map[string]any)["date"] != "2026-11-21" || days[0].(map[string]any)["runs"] != 2.0 || days[1].(map[string]any)["runs"] != 1.0 {
		t.Fatalf("days = %v", days)
	}

	today := admin.mustOK(admin.do("GET", "/api/admin/usage/users?from=2026-11-21&to=2026-11-21", nil)).list("users")
	if len(today) != 2 {
		t.Fatalf("readers with usage today = %v", today)
	}
	for _, u := range today {
		if u := u.(map[string]any); u["runs"] != 1.0 || !near(u["credits"], 6.2) {
			t.Errorf("today, %v", u)
		}
	}
	expectError(t, admin.do("GET", "/api/admin/usage?from=2026-11-21&to=2026-11-20", nil), http.StatusBadRequest, "bad_range")
}

func TestOnlyTheAdminSeesPlansAndUsage(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	bob := a.user("bob")
	id := me(t, bob)["id"]
	for _, r := range []resp{
		bob.do("GET", "/api/admin/plans", nil),
		bob.do("POST", "/api/admin/plans", map[string]any{"name": "x", "daily": 1}),
		bob.do("GET", "/api/admin/usage", nil),
		bob.do("GET", "/api/admin/usage/users", nil),
		bob.do("GET", fmt.Sprintf("/api/admin/usage/users/%v", id), nil),
		bob.do("PUT", fmt.Sprintf("/api/admin/users/%v", id), map[string]any{"planId": 1}),
	} {
		expectError(t, r, http.StatusForbidden, "forbidden")
	}
}
