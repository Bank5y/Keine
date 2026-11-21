package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/server"
)

func explorationsPath(courseID, lessonID int) string {
	return fmt.Sprintf("/api/courses/%d/lessons/%d/explorations", courseID, lessonID)
}

// analysisID runs an analysis to the end and returns the ID its done
// event carries.
func analysisID(t *testing.T, c *client, courseID, lessonID int, quote map[string]any) string {
	t.Helper()
	events, r := analyze(t, c, courseID, lessonID, quote)
	expectStatus(t, r, http.StatusOK)
	done := lastEvent(t, events)
	id, _ := done.data["analysisId"].(string)
	if done.name != "done" || id == "" {
		t.Fatalf("expected done with an analysis ID, got %+v", done)
	}
	return id
}

func pin(c *client, courseID, lessonID int, analysisID string) resp {
	return c.do("POST", explorationsPath(courseID, lessonID), map[string]any{"analysisId": analysisID})
}

func explorations(t *testing.T, c *client, courseID, lessonID int) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range c.mustOK(c.do("GET", explorationsPath(courseID, lessonID), nil)).list("explorations") {
		out = append(out, e.(map[string]any))
	}
	return out
}

// pinnedRows reads, past the API, what pinning wrote besides the
// exploration: no endpoint shows the snapshot, the nodes or the turns yet.
type pinnedRows struct {
	snapshot  string
	nodes     []string // kind/name
	turns     int
	reply     string
	turnModel string
}

func readPinned(t *testing.T, a *app, explorationID int) pinnedRows {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var p pinnedRows
	if err := conn.QueryRow(ctx, "SELECT snapshot::text FROM explorations WHERE id = $1", explorationID).Scan(&p.snapshot); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	rows, err := conn.Query(ctx, "SELECT kind || '/' || name FROM exploration_nodes WHERE exploration_id = $1 ORDER BY id", explorationID)
	if err != nil {
		t.Fatalf("nodes: %v", err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		p.nodes = append(p.nodes, n)
	}
	rows.Close()
	if err := conn.QueryRow(ctx, `SELECT count(*), coalesce(min(t.reply), ''), coalesce(min(t.meta->>'model'), '')
		FROM exploration_turns t JOIN exploration_nodes n ON n.id = t.node_id WHERE n.exploration_id = $1`, explorationID).Scan(&p.turns, &p.reply, &p.turnModel); err != nil {
		t.Fatalf("turns: %v", err)
	}
	return p
}

func TestAPinnedAnalysisBecomesAnExplorationOfTheLesson(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	r := pin(bob, id, lesson, analysisID(t, bob, id, lesson, agentQuote))
	expectStatus(t, r, http.StatusCreated)
	created := r.json()
	if created["title"] != "Agent（智能体）" || created["intro"] != "能自主规划步骤、调用工具的大模型应用。" || created["quote"] != "Agent" {
		t.Fatalf("created %+v", created)
	}

	list := explorations(t, bob, id, lesson)
	if len(list) != 1 {
		t.Fatalf("expected the pinned exploration in the list, got %+v", list)
	}
	e := list[0]
	if e["id"] != created["id"] || e["title"] != "Agent（智能体）" || e["intro"] != created["intro"] {
		t.Fatalf("listed %+v", e)
	}
	rg, _ := e["range"].(map[string]any)
	// the second "Agent" of the lesson, after the first paragraph's text
	if rg == nil || rg["end"].(float64)-rg["start"].(float64) != 5 {
		t.Fatalf("range %+v", e["range"])
	}
	p := readPinned(t, a, int(created["id"].(float64)))
	if !strings.Contains(p.snapshot, "这里的 <selected>Agent</selected> 指的是") || !strings.Contains(p.snapshot, "analysis-") {
		t.Fatalf("snapshot %s", p.snapshot)
	}
	if len(p.nodes) != 1 || !strings.HasPrefix(p.nodes[0], "main/") {
		t.Fatalf("nodes %v", p.nodes)
	}
	if p.turns != 1 || !strings.Contains(p.reply, "<exp_title>Agent（智能体）</exp_title>") || p.turnModel != "m-1" {
		t.Fatalf("first turn %+v", p)
	}
}

// sharedAnalysisColumn is analysisColumn published, so other readers read
// it too.
func sharedAnalysisColumn(t *testing.T, a *app, alice *client) (courseID, lessonID int) {
	t.Helper()
	courseID, lessonID = analysisColumn(t, alice)
	alice.mustOK(requestPublish(alice, courseID))
	admin := a.admin()
	admin.mustOK(admin.do("POST", fmt.Sprintf("/api/admin/reviews/%d/approve", reviewID(t, admin, courseID)), nil))
	return courseID, lessonID
}

func TestAnAnalysisCanBePinnedForHalfAnHour(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	m := newScriptedModel(texts(goodAnswer), texts(goodAnswer))
	a := aiAppWith(t, m, false, func(o *server.Options) { o.Now = clock.Now })
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	late := analysisID(t, bob, id, lesson, agentQuote)
	clock.advance(30 * time.Minute)
	expectError(t, pin(bob, id, lesson, late), http.StatusGone, "analysis_expired")

	inTime := analysisID(t, bob, id, lesson, agentQuote)
	clock.advance(29 * time.Minute)
	expectStatus(t, pin(bob, id, lesson, inTime), http.StatusCreated)
	// once pinned, it is no longer there to pin
	expectError(t, pin(bob, id, lesson, inTime), http.StatusGone, "analysis_expired")
}

func TestANewAnalysisReplacesTheOneNotPinned(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	old := analysisID(t, bob, id, lesson, agentQuote)
	latest := analysisID(t, bob, id, lesson, agentQuote)
	expectError(t, pin(bob, id, lesson, old), http.StatusGone, "analysis_expired")
	expectStatus(t, pin(bob, id, lesson, latest), http.StatusCreated)
}

func TestOnlyItsReaderPinsAnAnalysisAndOnlyOnItsLesson(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	alice := a.user("alice")
	id, lesson := sharedAnalysisColumn(t, a, bob)

	analysis := analysisID(t, bob, id, lesson, agentQuote)
	expectError(t, pin(alice, id, lesson, analysis), http.StatusGone, "analysis_expired")
	expectError(t, pin(bob, id, lessonIDs(t, bob, id)["l00"], analysis), http.StatusGone, "analysis_expired")
	if list := explorations(t, alice, id, lesson); len(list) != 0 {
		t.Fatalf("alice got an exploration: %+v", list)
	}
	// still bob's to pin
	expectStatus(t, pin(bob, id, lesson, analysis), http.StatusCreated)
}

func TestAnAnalysisOverlappingAnExplorationMadeMeanwhileIsNotPinned(t *testing.T) {
	var a *app
	// while the model answers, another tab of the reader's pins an
	// exploration over the same text: written past the API, since the
	// analysis that tab pinned was replaced when this one started
	meanwhile := texts(goodAnswer)
	meanwhile.before = func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, a.dbURL)
		if err != nil {
			panic(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `INSERT INTO explorations (user_id, course_id, lesson_key, quote, prefix, suffix, title, intro, meaning, snapshot)
			SELECT u.id, c.id, 'l01', '这里的 Agent', '', '', '别处', '别处固定的。', '', '{}' FROM users u, courses c WHERE u.username = 'bob'`); err != nil {
			panic(err)
		}
	}
	m := newScriptedModel(meanwhile)
	a = aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)

	analysis := analysisID(t, bob, id, lesson, agentQuote)
	expectError(t, pin(bob, id, lesson, analysis), http.StatusConflict, "overlap")
	if list := explorations(t, bob, id, lesson); len(list) != 1 || list[0]["title"] != "别处" {
		t.Fatalf("expected only the exploration made meanwhile, got %+v", list)
	}
}

func TestASelectionOverlappingAnExplorationIsNotAnalyzed(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts(goodAnswer))
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)
	expectStatus(t, pin(bob, id, lesson, analysisID(t, bob, id, lesson, agentQuote)), http.StatusCreated)

	_, r := analyze(t, bob, id, lesson, map[string]any{"quote": "这里的 Agent 指的是", "prefix": "## 一个最小的例子\n\n", "suffix": "把目标"})
	expectError(t, r, http.StatusConflict, "overlap")
	if len(m.calls()) != 1 {
		t.Fatalf("the overlapping selection reached the model")
	}
	// the other Agent, outside the exploration, is still analyzed
	analysisID(t, bob, id, lesson, map[string]any{"quote": "Agent", "prefix": "", "suffix": " 不是一个新概念"})
}

func TestAFailedAnalysisHasNoIDAndLeavesNothingToPin(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("没有标签。"), texts("还是没有标签。"), unavailable(), unavailable())
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson := analysisColumn(t, bob)
	before := analysisID(t, bob, id, lesson, agentQuote)

	for _, want := range []string{"malformed_output", "provider_error"} {
		events, _ := analyze(t, bob, id, lesson, agentQuote)
		last := lastEvent(t, events)
		if last.name != "error" || last.data["code"] != want {
			t.Fatalf("expected %s, got %+v", want, last)
		}
		for _, e := range events {
			if _, ok := e.data["analysisId"]; ok {
				t.Fatalf("a failed analysis gave an ID: %+v", e)
			}
		}
	}
	// the analysis before was replaced when the failed one started
	expectError(t, pin(bob, id, lesson, before), http.StatusGone, "analysis_expired")
}
