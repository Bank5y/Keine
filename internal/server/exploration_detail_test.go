package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

func explorationPath(id int) string { return fmt.Sprintf("/api/explorations/%d", id) }

func TestAnExplorationShowsItsDocumentAndItsNodeTree(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	d := bob.mustOK(bob.do("GET", explorationPath(exp), nil)).json()
	if d["title"] != "Agent（智能体）" || d["intro"] != "能自主规划步骤、调用工具的大模型应用。" ||
		d["meaning"] != "Agent 是把目标拆成步骤、逐步执行的系统。\n\n在这一讲里，它指把目标拆成步骤的程序。" {
		t.Fatalf("document head %v", d)
	}
	if d["lessonTitle"] != "01｜用户输入规范化" || d["quote"] != "Agent" {
		t.Fatalf("where it was made %v", d)
	}
	// the sections after the meaning are empty until a sync fills them
	for _, k := range []string{"footnotes", "extensions", "expansions", "references"} {
		if l, ok := d[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s: expected an empty list, got %v", k, d[k])
		}
	}
	nodes, _ := d["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("expected the main node only, got %v", d["nodes"])
	}
	main := nodes[0].(map[string]any)
	turns, _ := main["turns"].([]any)
	if main["kind"] != "main" || main["parentId"] != nil || len(turns) != 1 {
		t.Fatalf("main node %v", main)
	}
	q := turns[0].(map[string]any)["question"].(map[string]any)
	if att := q["attachments"].([]any); len(att) != 1 || att[0].(map[string]any)["text"] != "Agent" {
		t.Fatalf("the analysis turn's question carries the selection: %v", q)
	}
}

func nodeAndTurnCount(t *testing.T, a *app, exp int) (nodes, turns int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM exploration_nodes WHERE exploration_id = $1),
		(SELECT count(*) FROM exploration_turns t JOIN exploration_nodes n ON n.id = t.node_id WHERE n.exploration_id = $1)`, exp).Scan(&nodes, &turns); err != nil {
		t.Fatalf("count: %v", err)
	}
	return nodes, turns
}

func TestDeletingAnExplorationTakesItsDialogueWithIt(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	id, lesson, exp := pinnedExploration(t, bob)

	expectStatus(t, bob.do("DELETE", explorationPath(exp), nil), http.StatusNoContent)

	if list := explorations(t, bob, id, lesson); len(list) != 0 {
		t.Fatalf("expected it gone from the lesson, got %v", list)
	}
	expectStatus(t, bob.do("GET", explorationPath(exp), nil), http.StatusNotFound)
	if n, tr := nodeAndTurnCount(t, a, exp); n != 0 || tr != 0 {
		t.Fatalf("expected its nodes and turns gone, got %d nodes, %d turns", n, tr)
	}
}

func TestOnlyItsReaderSeesOrDeletesAnExploration(t *testing.T) {
	a := aiApp(t, newScriptedModel(texts(goodAnswer)))
	bob := a.user("bob")
	carol := a.user("carol")
	_, _, exp := pinnedExploration(t, bob)

	expectStatus(t, carol.do("GET", explorationPath(exp), nil), http.StatusNotFound)
	expectStatus(t, carol.do("DELETE", explorationPath(exp), nil), http.StatusNotFound)
	expectStatus(t, bob.do("DELETE", explorationPath(exp+1000), nil), http.StatusNotFound)
	bob.mustOK(bob.do("GET", explorationPath(exp), nil))
}
