package server_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// openNode opens a node of kind on turn fork and returns the node kept.
func openNode(t *testing.T, c *client, exp, fork int, kind, text string, fragment ...string) map[string]any {
	t.Helper()
	events, r := postEvents(t, c, fmt.Sprintf("/api/explorations/%d/nodes", exp), map[string]any{"kind": kind, "fork": fork, "question": question(text, fragment...)})
	expectStatus(t, r, http.StatusOK)
	last := lastEvent(t, events)
	n, _ := last.data["result"].(map[string]any)
	if last.name != "done" || n == nil {
		t.Fatalf("expected the node opened, got %+v", last)
	}
	return n
}

// dialogueOf is what a request holds after the first exchange (the
// first question and the analysis): every user and assistant message, as
// role:content.
func dialogueOf(t *testing.T, msgs []string) []string {
	t.Helper()
	i := slices.IndexFunc(msgs, func(m string) bool { return strings.HasPrefix(m, "assistant:") })
	if i < 0 {
		t.Fatalf("no first reply in %q", msgs)
	}
	return msgs[i+1:]
}

func TestANodeSeesItsChainUpToWhereItForked(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("答二"), texts("答三"), texts("兄弟的答"), texts("子节点的答"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	t2, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "问二")
	ask(t, bob, exp, idOf(t2), "问三")
	openNode(t, bob, exp, idOf(t2), "branch", "兄弟节点的问题")

	n := openNode(t, bob, exp, idOf(t2), "child", "沿着这一点继续往下讲", "选择权集中在规划阶段")

	calls := m.calls()
	got := dialogueOf(t, contents(calls[len(calls)-1]))
	want := []string{"user:问二", "assistant:答二", "user:【选中片段】\n「选择权集中在规划阶段」\n\n沿着这一点继续往下讲"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected only the chain up to turn 2, got %q", got)
	}
	if n["kind"] != "child" || n["name"] != "选择权集中在规划阶段" {
		t.Fatalf("node %v", n)
	}
	att := turnsOf(n)[0]["question"].(map[string]any)["attachments"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["text"] != "选择权集中在规划阶段" {
		t.Fatalf("the selection is on the node's first question: %v", att)
	}
}

func TestNestedNodesSeeTheWholeChainOfForks(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("答二"), texts("A1"), texts("A2"), texts("B1"), texts("C1"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	t2, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "问二")
	na := openNode(t, bob, exp, idOf(t2), "child", "节点 A 第一问")
	a2, _ := ask(t, bob, exp, idOf(turnsOf(na)[0]), "节点 A 第二问")
	nb := openNode(t, bob, exp, idOf(turnsOf(na)[0]), "divergent", "节点 B 第一问")
	openNode(t, bob, exp, idOf(turnsOf(nb)[0]), "branch", "节点 C 第一问")
	_ = a2

	calls := m.calls()
	got := dialogueOf(t, contents(calls[len(calls)-1]))
	want := []string{"user:问二", "assistant:答二", "user:节点 A 第一问", "assistant:A1", "user:节点 B 第一问", "assistant:B1", "user:节点 C 第一问"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected the three forks' chain, without A's second turn, got %q", got)
	}
	if n := nodeOf(t, detail(t, bob, exp), idOf(nb)); n["parentId"].(float64) != na["id"].(float64) {
		t.Fatalf("B forks from A: %v", n)
	}
}

func TestDeletingANodeTakesTheNodesUnderIt(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("A1"), texts("B1"), texts("C1"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	na := openNode(t, bob, exp, first, "child", "A")
	openNode(t, bob, exp, idOf(turnsOf(na)[0]), "child", "B under A")
	nc := openNode(t, bob, exp, first, "branch", "C")

	expectStatus(t, bob.do("DELETE", fmt.Sprintf("/api/explorations/%d/nodes/%d", exp, idOf(na)), nil), http.StatusNoContent)

	d := detail(t, bob, exp)
	var left []string
	for _, n := range d["nodes"].([]any) {
		left = append(left, n.(map[string]any)["name"].(string))
	}
	if len(left) != 2 || left[1] != nc["name"] {
		t.Fatalf("expected the main node and C left, got %v", left)
	}
	main := nodeOf(t, d, 0)
	expectError(t, bob.do("DELETE", fmt.Sprintf("/api/explorations/%d/nodes/%d", exp, idOf(main)), nil), http.StatusConflict, "main_node")
}

func TestAnExplorationHoldsFiftyNodes(t *testing.T) {
	steps := []step{texts(goodAnswer)}
	for range 49 {
		steps = append(steps, texts("答"))
	}
	m := newScriptedModel(steps...)
	a := aiApp(t, m)
	bob := a.user("bob")
	id, lesson, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)
	for i := range 49 {
		openNode(t, bob, exp, first, "child", fmt.Sprintf("第 %d 个", i+2))
	}

	_, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/nodes", exp), map[string]any{"kind": "child", "fork": first, "question": question("第 51 个")})
	expectError(t, r, http.StatusConflict, "node_limit")
	if got := explorations(t, bob, id, lesson)[0]["nodes"]; got != float64(49) {
		t.Fatalf("the card counts the nodes but the main one: %v", got)
	}
}
