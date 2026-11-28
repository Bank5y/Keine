package assistant_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
)

// call is one recorded model call: the text it streams, how it finished
// and the output tokens it reports.
type call struct {
	text   string
	finish assistant.FinishReason
	output int64
}

// script answers each model call with the next of its calls, and keeps
// the requests. Like most adapters it cannot continue a reply as written,
// so it asks to; exact is one that can. The nth request (from 0) fails
// with fail[n] instead, taking no call.
type script struct {
	calls    []call
	requests []assistant.Request
	exact    bool
	fail     map[int]error
}

func (s *script) Stream(_ context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	s.requests = append(s.requests, req)
	held := func() error { return nil }
	if !s.exact {
		req, emit, held = assistant.AskToContinue(req, emit)
	}
	if err := s.fail[len(s.requests)-1]; err != nil {
		return assistant.Result{}, err
	}
	if len(s.calls) == 0 {
		return assistant.Result{}, &assistant.Error{Category: assistant.ErrUnavailable, Detail: "script ran out"}
	}
	c := s.calls[0]
	s.calls = s.calls[1:]
	// in pieces, as a stream comes
	for r := []rune(c.text); len(r) > 0; {
		n := min(len(r), 7)
		if err := emit(assistant.Event{Kind: assistant.EventText, Text: string(r[:n])}); err != nil {
			return assistant.Result{}, err
		}
		r = r[n:]
	}
	if err := held(); err != nil {
		return assistant.Result{}, err
	}
	in, out := int64(1000), c.output
	return assistant.Result{Finish: c.finish, Usage: assistant.Usage{Input: &in, Output: &out}}, nil
}

func turn(s *script) assistant.Turn {
	return assistant.Turn{Adapter: s, Model: "m", ContextWindow: 128_000}
}

var lesson = assistant.ConceptLesson{CourseTitle: "课", Chapter: "一", Title: "讲", Markdown: "正文"}

// A concept array cut off at the cap, in three recorded pieces.
const (
	conceptsHead = `[{"name": "极限", "aliases": ["limit"], "section": "", "summary": "数列趋近的值"},
 {"name": "连续", "aliases": [], "section": "", "summa`
	conceptsMiddle = `ry": "没有断点的函数"},
 {"name": "导数", "aliases": ["derivative"], "sect`
	conceptsTail = `ion": "", "summary": "变化率"}]`
)

func TestExtractConceptsContinuesWhatTheCapCutOff(t *testing.T) {
	s := &script{calls: []call{
		{conceptsHead, assistant.FinishLength, 100},
		{conceptsMiddle, assistant.FinishLength, 100},
		{conceptsTail, assistant.FinishStop, 50},
	}}
	x, meta, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if err != nil {
		t.Fatal(err)
	}
	got := x.Concepts
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "极限,连续,导数" || got[1].Summary != "没有断点的函数" {
		t.Fatalf("concepts = %+v", got)
	}
	// each continuation goes on from all that was written
	if len(s.requests) != 3 {
		t.Fatalf("%d calls", len(s.requests))
	}
	last := s.requests[2].Messages[len(s.requests[2].Messages)-1]
	if last.Role != assistant.RoleAssistant || !last.Prefix || last.Content != conceptsHead+conceptsMiddle {
		t.Fatalf("third call ends with %+v", last)
	}
	// every call is charged
	if meta.Usage.Output == nil || *meta.Usage.Output != 250 || *meta.Usage.Input != 3000 {
		t.Fatalf("usage = %+v", meta.Usage)
	}
}

const (
	reportHead = "<rep_title>极限的故事</rep_title><rep_summary>从数列讲到函数。</rep_summary><rep_body>先看数列：一个数列越来越靠近某个数，"
	// a model asked to go on repeats the end of what it wrote
	reportRest = "越来越靠近某个数，这个数就是它的极限。</rep_body><rep_points>- 数列的极限\n- 函数的极限</rep_points>"
)

func TestWriteReportContinuesWhatTheCapCutOff(t *testing.T) {
	s := &script{calls: []call{
		{reportHead, assistant.FinishLength, 100},
		{reportRest, assistant.FinishStop, 100},
	}}
	var streamed strings.Builder
	sink := assistant.Sink{
		Delta:    func(d assistant.Delta) error { streamed.WriteString(d.Text); return nil },
		Activity: func(assistant.Activity) error { return nil },
	}
	msgs := []assistant.Message{{Role: assistant.RoleUser, Content: "对话"}}
	rep, meta, err := assistant.WriteReport(context.Background(), assistant.Run{Turn: turn(s)}, msgs, sink)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Body != "先看数列：一个数列越来越靠近某个数，这个数就是它的极限。" || rep.Points != "- 数列的极限\n- 函数的极限" {
		t.Fatalf("report = %+v", rep)
	}
	if want := "极限的故事从数列讲到函数。" + rep.Body + rep.Points; streamed.String() != want {
		t.Fatalf("streamed %q", streamed.String())
	}
	if *meta.Usage.Output != 200 {
		t.Fatalf("usage = %+v", meta.Usage)
	}
}

func TestLongOutputGivesUpAfterMaxContinues(t *testing.T) {
	calls := []call{{conceptsHead, assistant.FinishLength, 100}}
	for range assistant.MaxContinues {
		calls = append(calls, call{`{"name": "x", "summary": "y"},`, assistant.FinishLength, 100})
	}
	s := &script{calls: calls}
	_, meta, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if re, ok := errors.AsType[*assistant.RunError](err); !ok || re.Code != assistant.CodeOutputTruncated {
		t.Fatalf("err = %v", err)
	}
	if len(s.requests) != 1+assistant.MaxContinues || *meta.Usage.Output != int64(100*len(calls)) {
		t.Fatalf("%d calls, usage %+v", len(s.requests), meta.Usage)
	}
}

func TestLongOutputCapIsTheModels(t *testing.T) {
	for _, c := range []struct {
		max      int
		thinking bool
		want     int
	}{{0, false, 8192}, {0, true, 12288}, {32000, false, 32000}, {32000, true, 32000}} {
		s := &script{calls: []call{{`[]`, assistant.FinishStop, 1}}}
		tn := turn(s)
		tn.MaxOutput, tn.Thinking = c.max, c.thinking
		if _, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: tn}, lesson); err != nil {
			t.Fatal(err)
		}
		if got := s.requests[0].MaxTokens; got != c.want {
			t.Errorf("max output %d, thinking %v: asked for %d, want %d", c.max, c.thinking, got, c.want)
		}
	}
}

func TestAnalysisKeepsItsCapAndFailsWhenCutOff(t *testing.T) {
	s := &script{calls: []call{{"<exp_title>标题</exp_title><exp_concept>半截", assistant.FinishLength, 4096}}}
	tn := turn(s)
	tn.MaxOutput = 32000
	sink := assistant.Sink{Delta: func(assistant.Delta) error { return nil }, Activity: func(assistant.Activity) error { return nil }}
	_, _, err := assistant.Analyze(context.Background(), assistant.Run{Turn: tn}, []assistant.Message{{Role: assistant.RoleUser, Content: "问"}}, sink)
	if re, ok := errors.AsType[*assistant.RunError](err); !ok || re.Code != assistant.CodeOutputTruncated {
		t.Fatalf("err = %v", err)
	}
	if len(s.requests) != 1 || s.requests[0].MaxTokens != 4096 {
		t.Fatalf("%d calls, max tokens %d", len(s.requests), s.requests[0].MaxTokens)
	}
}

func TestAModelThatContinuesAsWrittenKeepsWhatLooksRepeated(t *testing.T) {
	// a rule of dashes cut in the middle: its second half is no repetition
	head, rest := `[{"name": "极限", "aliases": [], "section": "", "summary": "---------`, "---------\"}]"
	s := &script{exact: true, calls: []call{{head, assistant.FinishLength, 10}, {rest, assistant.FinishStop, 10}}}
	x, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if err != nil {
		t.Fatal(err)
	}
	got := x.Concepts
	if len(got) != 1 || got[0].Summary != "------------------" {
		t.Fatalf("concepts = %+v", got)
	}
}

func TestAContinuationThatFailsBeforeWritingIsTriedAgain(t *testing.T) {
	s := &script{calls: []call{
		{conceptsHead, assistant.FinishLength, 100},
		{conceptsMiddle + conceptsTail, assistant.FinishStop, 100},
	}, fail: map[int]error{1: &assistant.Error{Category: assistant.ErrRateLimited, Detail: "429"}}}
	got, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Concepts) != 3 || len(s.requests) != 3 {
		t.Fatalf("%d concepts in %d calls", len(got.Concepts), len(s.requests))
	}
}

func TestACutOffAnswerIsNotContinuedPastTheWindow(t *testing.T) {
	s := &script{calls: []call{{conceptsHead, assistant.FinishLength, 100}}}
	tn := turn(s)
	// the lesson and what was written leave no room for another call
	tn.ContextWindow, tn.MaxOutput = 2100, 2000
	_, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: tn}, lesson)
	if re, ok := errors.AsType[*assistant.RunError](err); !ok || re.Code != assistant.CodeOutputTruncated {
		t.Fatalf("err = %v", err)
	}
	if len(s.requests) != 1 {
		t.Fatalf("%d calls", len(s.requests))
	}
}

func TestAnExtractionGivesTheLessonsSummaryAndEachConceptsRole(t *testing.T) {
	reply := `{"summary": "这一讲讲导数。", "concepts": [{"name": "导数", "aliases": [], "section": "", "role": "define", "summary": "变化率"}, {"name": "极限", "aliases": [], "section": "", "role": "use", "summary": "趋近的值"}, {"name": "连续", "aliases": [], "section": "", "summary": "没有断点"}]}`
	s := &script{calls: []call{{reply, assistant.FinishStop, 50}}}
	x, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if err != nil {
		t.Fatal(err)
	}
	if x.Summary != "这一讲讲导数。" || len(x.Concepts) != 3 {
		t.Fatalf("extraction = %+v", x)
	}
	// a role left out is taken as defined there
	if x.Concepts[0].Role != assistant.RoleDefine || x.Concepts[1].Role != assistant.RoleUse || x.Concepts[2].Role != assistant.RoleDefine {
		t.Fatalf("roles = %+v", x.Concepts)
	}
}

func TestAnExtractionObjectWithoutConceptsIsMalformed(t *testing.T) {
	s := &script{calls: []call{{`{"summary": "这一讲讲导数。"}`, assistant.FinishStop, 50}}}
	_, _, err := assistant.ExtractConcepts(context.Background(), assistant.Run{Turn: turn(s)}, lesson)
	if re, ok := errors.AsType[*assistant.RunError](err); !ok || re.Code != assistant.CodeMalformedOutput {
		t.Fatalf("err = %v", err)
	}
}
