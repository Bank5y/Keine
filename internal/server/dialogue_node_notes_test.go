package server_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/jackc/pgx/v5"
)

func TestANodesSecondTurnStartsWithItsFirstRequestAndReplyAndAddsNoNote(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("子节点的第一个回复。"), texts("答二"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	n := openNode(t, bob, exp, firstTurn(t, bob, exp), "child", "往深处讲讲")

	ask(t, bob, exp, idOf(turnsOf(n)[0]), "那边界情况呢？")

	calls := m.calls()
	first, second := contents(calls[1]), contents(calls[2])
	want := append(append([]string{}, first...), "assistant:子节点的第一个回复。", "user:那边界情况呢？")
	if !slices.Equal(second, want) {
		t.Fatalf("expected the first request, its reply, then the bare question;\nwant %q\ngot  %q", want, second)
	}
	if lastQuestion(t, first) != nodeNotes["child"]+"\n\n往深处讲讲" {
		t.Fatalf("the fork turn carries its note: %q", first)
	}
}

// The notes a node's first turn begins with, by kind (#164, ADR 0011).
var nodeNotes = map[string]string{
	"child":     "此次作答沿上面讨论的这一点往深处讲：机制、原因、细节和边界情况。",
	"branch":    "此次作答从上面讨论的概念横向展开：讲和它相关的概念，以及它们之间的关系。",
	"divergent": "此次作答跳出当前主题：讲同类的做法和其他领域的类比，并和这里的做法对照；需要时用网络搜索。",
}

// lastQuestion is what the last message of a request asks.
func lastQuestion(t *testing.T, msgs []string) string {
	t.Helper()
	q, ok := strings.CutPrefix(msgs[len(msgs)-1], "user:")
	if !ok {
		t.Fatalf("a request ends with a question: %q", msgs)
	}
	return q
}

// hasNote tells whether any message holds a node's note.
func hasNote(msgs []string) bool {
	for _, m := range msgs {
		for _, n := range nodeNotes {
			if strings.Contains(m, n) || strings.Contains(m, "此次作答") {
				return true
			}
		}
	}
	return false
}

func TestTheMainNodesTurnsCarryNoNote(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("答一"), texts("答二"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	t1, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "问一")

	ask(t, bob, exp, idOf(t1), "问二")

	for i, c := range m.calls()[1:] {
		if msgs := contents(c); hasNote(msgs) {
			t.Fatalf("request %d of the main node holds a note: %q", i+1, msgs)
		}
	}
}

func TestTheDetailShowsTheQuestionsAsTheReaderAskedThem(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("分支的答"), texts("答二"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	b := openNode(t, bob, exp, firstTurn(t, bob, exp), "divergent", "别的领域怎么做？", "选中的一句")
	ask(t, bob, exp, idOf(turnsOf(b)[0]), "再举一个例子")

	raw := bob.mustOK(bob.do("GET", explorationPath(exp), nil)).body
	if hasNote([]string{string(raw)}) {
		t.Fatalf("the detail shows no note: %s", raw)
	}
	turns := turnsOf(nodeOf(t, detail(t, bob, exp), idOf(b)))
	if q := turns[0]["question"].(map[string]any); q["text"] != "别的领域怎么做？" {
		t.Fatalf("the fork turn's question is the reader's: %v", q)
	}
	if turnsOf(b)[0]["question"].(map[string]any)["text"] != "别的领域怎么做？" {
		t.Fatalf("the node opened shows no note: %v", b)
	}
}

func TestAForkTurnTriedAgainAsksWithItsNoteAgain(t *testing.T) {
	down := step{err: &assistant.Error{Category: assistant.ErrUnavailable, Detail: "down"}}
	m := newScriptedModel(texts(goodAnswer), down, down, texts("重试后的回复。"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	events, r := postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/nodes", exp), map[string]any{"kind": "branch", "fork": firstTurn(t, bob, exp), "question": question("相关的概念")})
	expectStatus(t, r, http.StatusOK)
	n, _ := lastEvent(t, events).data["result"].(map[string]any)
	if n == nil {
		t.Fatalf("expected the node kept with its failed turn: %+v", lastEvent(t, events))
	}

	events, r = postEvents(t, bob, fmt.Sprintf("/api/explorations/%d/turns/%d/retry", exp, idOf(turnsOf(n)[0])), map[string]any{})
	expectStatus(t, r, http.StatusOK)
	if last := lastEvent(t, events); last.name != "done" {
		t.Fatalf("retry ended with %+v", last)
	}

	calls := m.calls()
	if got := lastQuestion(t, contents(calls[len(calls)-1])); got != nodeNotes["branch"]+"\n\n相关的概念" {
		t.Fatalf("the retry asks as the fork turn did: %q", got)
	}
}

func TestAChildOfABranchSeesEachForkWithItsOwnKindsNote(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("分支的答"), texts("子节点的答"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	b := openNode(t, bob, exp, firstTurn(t, bob, exp), "branch", "还有哪些相关的概念？")

	openNode(t, bob, exp, idOf(turnsOf(b)[0]), "child", "第二个概念细讲一下")

	calls := m.calls()
	got := dialogueOf(t, contents(calls[len(calls)-1]))
	want := []string{
		"user:" + nodeNotes["branch"] + "\n\n还有哪些相关的概念？",
		"assistant:分支的答",
		"user:" + nodeNotes["child"] + "\n\n第二个概念细讲一下",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("want %q\ngot  %q", want, got)
	}
}

func TestTheTurnANodeForksWithBeginsWithItsKindsNote(t *testing.T) {
	for kind, note := range nodeNotes {
		t.Run(kind, func(t *testing.T) {
			m := newScriptedModel(texts(goodAnswer), texts("答"))
			a := aiApp(t, m)
			bob := a.user("bob")
			_, _, exp := pinnedExploration(t, bob)

			openNode(t, bob, exp, firstTurn(t, bob, exp), kind, "接着问", "选中的一句")

			calls := m.calls()
			got := lastQuestion(t, contents(calls[len(calls)-1]))
			want := note + "\n\n【选中片段】\n「选中的一句」\n\n接着问"
			if got != want {
				t.Fatalf("expected %q, got %q", want, got)
			}
		})
	}
}

// setForkNote rewrites, past the API, the node note kept with a turn; nil
// takes it out, as on a turn kept before notes were.
func setForkNote(t *testing.T, a *app, turn int, note *string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, a.dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	q := "UPDATE exploration_turns SET meta = meta - 'nodeNote' WHERE id = $1"
	args := []any{turn}
	if note != nil {
		q = "UPDATE exploration_turns SET meta = jsonb_set(meta, '{nodeNote}', to_jsonb($2::text)) WHERE id = $1"
		args = append(args, *note)
	}
	if _, err := conn.Exec(ctx, q, args...); err != nil {
		t.Fatalf("set note: %v", err)
	}
}

func TestAForkTurnKeepsItsNoteAsItWasAsked(t *testing.T) {
	m := newScriptedModel(texts(goodAnswer), texts("分支的答"), texts("答二"), texts("答三"))
	a := aiApp(t, m)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	b := openNode(t, bob, exp, firstTurn(t, bob, exp), "branch", "还有哪些相关的概念？")
	fork := idOf(turnsOf(b)[0])
	metas := turnMetas(t, a, exp)
	if got := metas[len(metas)-1]["nodeNote"]; got != nodeNotes["branch"] {
		t.Fatalf("the fork turn keeps the note it was asked with: %v", got)
	}

	// the note's wording changed since: the old turn keeps its own
	old := "旧的说法：讲讲相关的概念。"
	setForkNote(t, a, fork, &old)
	t2, _ := ask(t, bob, exp, fork, "再说一个")
	got := dialogueOf(t, contents(m.calls()[2]))
	if got[0] != "user:"+old+"\n\n还有哪些相关的概念？" {
		t.Fatalf("history carries the note as it was kept: %q", got)
	}
	if raw := bob.mustOK(bob.do("GET", explorationPath(exp), nil)).body; strings.Contains(string(raw), old) {
		t.Fatalf("the reader never sees the note: %s", raw)
	}

	// a turn kept without a note gets its kind's
	setForkNote(t, a, fork, nil)
	ask(t, bob, exp, idOf(t2), "第三问")
	if got := dialogueOf(t, contents(m.calls()[3])); got[0] != "user:"+nodeNotes["branch"]+"\n\n还有哪些相关的概念？" {
		t.Fatalf("a turn without a kept note gets its kind's: %q", got)
	}
}
