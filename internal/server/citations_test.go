package server_test

import (
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
)

// Citations (ADR 0013): the dialogue's web sources are numbered for the
// whole exploration, one number per address, and a turn may cite those
// along its chain.

// numbersOf is the citation number of each source a kept turn recorded, by
// address.
func numbersOf(t *testing.T, turn map[string]any) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, s := range turn["meta"].(map[string]any)["sources"].([]any) {
		m := s.(map[string]any)
		n, _ := m["n"].(float64)
		out[m["url"].(string)] = int(n)
	}
	return out
}

// lastAsked is the last user message of a request: the question, headed by
// what may be cited.
func lastAsked(req assistant.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == assistant.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

func TestADialogueSearchShowsItsResultsNumberedAndKeepsTheNumbers(t *testing.T) {
	t.Parallel()
	search := call("web_search", map[string]any{"query": "Raft 论文"})
	m := newScriptedModel(texts(goodAnswer), toolStep([]assistant.ToolCall{search}), texts("Raft 的论文见 [1]。"))
	a := deepseekApp(t, m, newFakeSearch())
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)

	kept, _ := ask(t, bob, exp, firstTurn(t, bob, exp), "找几篇 Raft 的论文并给出链接")

	got := toolResult(t, m.calls()[2], search)
	if !strings.Contains(got, "[1] Raft 论文") || !strings.Contains(got, "[2] 任期") {
		t.Fatalf("the results are not numbered: %q", got)
	}
	n := numbersOf(t, kept)
	if n["https://raft.github.io/raft.pdf"] != 1 || n["https://example.com/term"] != 2 {
		t.Fatalf("numbers kept %v", n)
	}
	if kept["reply"] != "Raft 的论文见 [1]。" {
		t.Fatalf("reply %v", kept["reply"])
	}
}

func TestTheTreeSharesNumbersAndATurnMayCiteOnlyItsChain(t *testing.T) {
	t.Parallel()
	f := newFakeSearch()
	f.search = func(n int, q string) ([]assistant.SearchHit, bool, error) {
		if q == "Paxos" {
			return []assistant.SearchHit{
				{Title: "任期", URL: "https://example.com/term", Summary: "又见任期。"},
				{Title: "Paxos Made Simple", URL: "https://lamport.org/paxos.pdf", Summary: "Paxos。"},
			}, true, nil
		}
		return []assistant.SearchHit{
			{Title: "Raft 论文", URL: "https://raft.github.io/raft.pdf", Summary: "Raft。"},
			{Title: "任期", URL: "https://example.com/term", Summary: "任期。"},
		}, true, nil
	}
	raft := call("web_search", map[string]any{"query": "Raft"})
	paxos := call("web_search", map[string]any{"query": "Paxos"})
	m := newScriptedModel(texts(goodAnswer),
		toolStep([]assistant.ToolCall{raft}), texts("见 [1]。"),
		toolStep([]assistant.ToolCall{paxos}), texts("见 [3]。"),
		texts("接着说 [1]。"))
	a := deepseekApp(t, m, f)
	bob := a.user("bob")
	_, _, exp := pinnedExploration(t, bob)
	first := firstTurn(t, bob, exp)

	main, _ := ask(t, bob, exp, first, "Raft 的论文")
	// a node opened on the analysis is the main line's sibling
	node := openNode(t, bob, exp, first, "divergent", "Paxos 的论文")
	ask(t, bob, exp, idOf(main), "再讲讲")

	got := numbersOf(t, turnsOf(node)[0])
	if got["https://example.com/term"] != 2 || got["https://lamport.org/paxos.pdf"] != 3 {
		t.Fatalf("the node does not number on from the tree: %v", got)
	}
	calls := m.calls()
	if q := lastAsked(calls[3]); strings.Contains(q, "Raft 论文") || strings.Contains(q, "可引用的资料") {
		t.Fatalf("the node is offered its sibling's sources: %q", q)
	}
	q := lastAsked(calls[5])
	if !strings.Contains(q, "[1] Raft 论文") || !strings.Contains(q, "[2] 任期") || strings.Contains(q, "Paxos") {
		t.Fatalf("a later turn of the main line is offered %q", q)
	}
	if strings.Contains(q, "https://") {
		t.Fatalf("the offer carries addresses: %q", q)
	}
}
