package anchor_test

import (
	"testing"

	"github.com/Bank5y/keine/internal/anchor"
)

const before = "Agent 是一个会思考的程序，它会调用工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。最后一段再说一次 Agent。"

// suggest is what the magic wand offers for quote q, taken from before, in
// the text after.
func suggest(t *testing.T, after string, q anchor.Quote) (anchor.Suggestion, bool) {
	t.Helper()
	return anchor.NewText(after).Suggest(q, anchor.NewText(before))
}

func expectSuggested(t *testing.T, after string, q anchor.Quote, want string) anchor.Suggestion {
	t.Helper()
	s, ok := suggest(t, after, q)
	if !ok {
		t.Fatalf("expected a suggestion %q, got none", want)
	}
	got := []rune(anchor.NewText(after).String())[s.Range.Start:s.Range.End]
	if string(got) != want || anchor.NewText(s.Quote.Exact).String() != want {
		t.Fatalf("expected %q suggested, got %q (%+v)", want, string(got), s)
	}
	if s.Confidence <= 0 || s.Confidence > 1 {
		t.Fatalf("confidence out of (0,1]: %v", s.Confidence)
	}
	return s
}

func TestSuggestsTheQuoteWithAChangedCharacter(t *testing.T) {
	after := "Agent 是一个会思索的程序，它会调用工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。最后一段再说一次 Agent。"
	expectSuggested(t, after, anchor.Quote{Exact: "会思考的程序", Prefix: "Agent是一个", Suffix: "，它会调用"}, "会思索的程序")
}

func TestSuggestsTheQuoteWithChangedPunctuation(t *testing.T) {
	after := "Agent 是一个会思考的程序。它会调用工具，完成任务！第二段也提到 Agent，以及 tool_call 这个字段。最后一段再说一次 Agent。"
	expectSuggested(t, after, anchor.Quote{Exact: "它会调用工具完成任务。", Prefix: "会思考的程序，", Suffix: "第二段"}, "它会调用工具，完成任务！")
}

func TestSuggestsTheRewordedQuoteBetweenItsSurroundings(t *testing.T) {
	after := "Agent 是一个会思考的程序，它会使用外部工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。最后一段再说一次 Agent。"
	expectSuggested(t, after, anchor.Quote{Exact: "调用工具", Prefix: "的程序，它会", Suffix: "完成任务。第二段"}, "使用外部工具")
}

func TestSuggestsNothingForADeletedQuote(t *testing.T) {
	after := "Agent 是一个会思考的程序，它会调用工具完成任务。最后一段再说一次 Agent。"
	if s, ok := suggest(t, after, anchor.Quote{Exact: "以及tool_call这个字段", Prefix: "第二段也提到Agent，", Suffix: "。最后一段"}); ok {
		t.Fatalf("expected no suggestion for deleted text, got %+v", s)
	}
}

func TestSuggestsNothingForAQuoteReplacedByOtherText(t *testing.T) {
	after := "Agent 是一个会思考的程序，它会调用工具完成任务。第二段改讲 MCP 协议的握手过程。最后一段再说一次 Agent。"
	if s, ok := suggest(t, after, anchor.Quote{Exact: "也提到Agent，以及tool_call这个字段", Prefix: "完成任务。第二段", Suffix: "。最后一段"}); ok {
		t.Fatalf("expected no suggestion for replaced text, got %+v", s)
	}
}

func TestTheSuggestionKeepsTheTextsSpacing(t *testing.T) {
	after := "Agent 是一个会思考的程序，它会调用 外部 工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。"
	s := expectSuggested(t, after, anchor.Quote{Exact: "调用工具", Prefix: "它会", Suffix: "完成任务"}, "调用外部工具")
	if s.Quote.Exact != "调用 外部 工具" || s.Quote.Prefix != "Agent 是一个会思考的程序，它会" || s.Quote.Suffix != "完成任务。第二段也提到 Agent，以及 tool_call 这个字段" {
		t.Fatalf("expected the quote as the text spells it, got %+v", s.Quote)
	}
}

func TestPositionDecidesBetweenEquallyGoodPlaces(t *testing.T) {
	// the quote, worded slightly differently, now sits twice in the text with
	// the same surroundings; the one where it used to be wins
	after := "Agent 是一个会思考的程序，它会调用工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。第三段：它会调用工具完成任务。"
	q := anchor.Quote{Exact: "调用工具完成任务", Prefix: "它会", Suffix: "。"}
	s, ok := anchor.NewText(after).Suggest(anchor.Quote{Exact: "调用了工具完成任务", Prefix: q.Prefix, Suffix: q.Suffix}, anchor.NewText("它会调用了工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。第三段：别的话"))
	if !ok || s.Range.Start != 17 {
		t.Fatalf("expected the first place suggested, got %+v %v", s, ok)
	}
}

func TestSuggestionsAreDeterministic(t *testing.T) {
	after := "Agent 是一个会思索的程序，它会使用外部工具完成任务。第二段也提到 Agent，以及 tool_call 这个字段。最后一段再说一次 Agent。"
	q := anchor.Quote{Exact: "调用工具", Prefix: "的程序，它会", Suffix: "完成任务。第二段"}
	first, _ := suggest(t, after, q)
	for range 20 {
		if s, _ := suggest(t, after, q); s != first {
			t.Fatalf("suggestions differ: %+v vs %+v", s, first)
		}
	}
}
