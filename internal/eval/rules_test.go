package eval

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// model outputs, each breaking one rule the way the analysis prototype
// (prototype/first-turn-analysis) saw models break it
const (
	good = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n在这一讲里，**任期**是每次选举开始时加一的编号。\n</exp_explanation>"

	withHeading = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n## 任期是什么\n\n每次选举开始时加一的编号。\n</exp_explanation>"

	withLink = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n详见 [Raft 论文](https://raft.github.io/raft.pdf) 第 5 节。\n</exp_explanation>"

	withToolCall = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>read_lesson\n</exp_explanation>"

	withSidewaysChart = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n```mermaid\nflowchart LR\n  A[跟随者] --> B[候选人]\n```\n</exp_explanation>"

	withBrokenChart = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n```mermaid\nflowchart TD\n  A[跟随者] -->\n```\n</exp_explanation>"

	englishTitle = "<exp_title>Term number</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n每次选举开始时加一的编号。\n</exp_explanation>"
)

const zhLesson = "Raft 把时间分成一个个任期，每个任期开始时进行选举。"
const enLesson = "Raft divides time into terms of arbitrary length, each beginning with an election."

// parses is a Mermaid parser that fails a diagram with a dangling arrow,
// as mermaid's own parser does.
type parses struct{ seen []string }

func (p *parses) Parse(src []string) ([]error, error) {
	p.seen = append(p.seen, src...)
	out := make([]error, len(src))
	for i, s := range src {
		if strings.HasSuffix(strings.TrimSpace(s), "-->") {
			out[i] = errors.New("Parse error on line 2")
		}
	}
	return out, nil
}

func rules(fs []Failure) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func TestAValidAnswerFailsNoRule(t *testing.T) {
	c := Case{Quote: "任期"}
	if fs := Check(good, c, zhLesson, &parses{}); len(fs) != 0 {
		t.Fatalf("failures: %v", fs)
	}
}

func TestEachRecordedOutputFailsTheServersCheck(t *testing.T) {
	for name, raw := range map[string]string{
		"a # heading":        withHeading,
		"a link written":     withLink,
		"tool call markup":   withToolCall,
		"a sideways diagram": withSidewaysChart,
	} {
		fs := Check(raw, Case{Quote: "任期"}, zhLesson, &parses{})
		if got := rules(fs); !slices.Equal(got, []string{RuleServer}) {
			t.Errorf("%s: rules %v (%v)", name, got, fs)
		}
	}
}

func TestADiagramMermaidCannotParseFails(t *testing.T) {
	p := &parses{}
	fs := Check(withBrokenChart, Case{Quote: "任期"}, zhLesson, p)
	if got := rules(fs); !slices.Equal(got, []string{RuleMermaid}) {
		t.Fatalf("rules %v (%v)", got, fs)
	}
	if len(p.seen) != 1 || !strings.HasPrefix(p.seen[0], "flowchart TD") {
		t.Fatalf("parser was given %q", p.seen)
	}
}

func TestTheTitleFollowsTheLanguageOfTheLesson(t *testing.T) {
	if got := rules(Check(englishTitle, Case{Quote: "任期"}, zhLesson, &parses{})); !slices.Equal(got, []string{RuleLanguage}) {
		t.Fatalf("an English title of a Chinese lesson: %v", got)
	}
	// a term selected as it is written may stay as it is
	termTitle := strings.Replace(englishTitle, "Term number", "epoll", 1)
	if fs := Check(termTitle, Case{Quote: "epoll"}, zhLesson, &parses{}); len(fs) != 0 {
		t.Fatalf("the selected term as the title: %v", fs)
	}
	if got := rules(Check(good, Case{Quote: "term"}, enLesson, &parses{})); !slices.Equal(got, []string{RuleLanguage}) {
		t.Fatalf("a Chinese title of an English lesson: %v", got)
	}
}

func TestKeywordsMustOrMustNotAppear(t *testing.T) {
	c := Case{Quote: "任期", MustInclude: []string{"选举", "TERM"}, MustNotInclude: []string{"日志复制"}}
	if fs := Check(good, c, zhLesson, &parses{}); len(fs) != 0 {
		t.Fatalf("keywords present (any case) and absent: %v", fs)
	}
	c = Case{Quote: "任期", MustInclude: []string{"心跳"}, MustNotInclude: []string{"递增"}}
	fs := Check(good, c, zhLesson, &parses{})
	if got := rules(fs); !slices.Equal(got, []string{RuleMustInclude, RuleMustNotInclude}) {
		t.Fatalf("rules %v", got)
	}
	for _, f := range fs {
		if !strings.Contains(f.Detail, "心跳") && !strings.Contains(f.Detail, "递增") {
			t.Fatalf("failure does not name its keyword: %v", f)
		}
	}
}

func TestNoAnswerAtAllFailsTheServersCheck(t *testing.T) {
	if got := rules(Check("", Case{Quote: "任期"}, zhLesson, &parses{})); !slices.Equal(got, []string{RuleServer}) {
		t.Fatalf("rules %v", got)
	}
}

func TestTheLanguageOfALessonIsItsProseNotItsCode(t *testing.T) {
	code := "```c\n" + strings.Repeat("struct list_head rdllist; /* ready list of the eventpoll */\n", 40) + "```\n"
	lesson := "# epoll 源码\n\n我们先看一下 epoll 中使用的数据结构。\n\n" + code + "\n这里的 `rdllist` 是就绪列表。\n"
	if fs := Check(good, Case{Quote: "rdllist"}, lesson, &parses{}); len(fs) != 0 {
		t.Fatalf("a Chinese lesson with long code: %v", fs)
	}
}

func TestAReplyCitesByNumberButWritesNoAddress(t *testing.T) {
	for _, ok := range []string{"Raft 的论文见 [1]，Paxos 见 [2][3]。", "两篇都讲了选举 [1, 2]。", "列表写作 `a[1]`。"} {
		if got := rules(CheckReply(ok, Turn{}, &parses{})); len(got) != 0 {
			t.Fatalf("%q broke %v", ok, got)
		}
	}
	for _, bad := range []string{"见 [Raft 论文](https://raft.github.io)。", "见 https://raft.github.io 。", "见 [1](https://raft.github.io)。"} {
		if got := rules(CheckReply(bad, Turn{}, &parses{})); !slices.Equal(got, []string{RuleReply}) {
			t.Fatalf("%q broke %v", bad, got)
		}
	}
}

func TestAReplyCitesOnlyWhatItWasOffered(t *testing.T) {
	if got := CheckCitations("见 [1] 和 [3]。", []int{1, 2}); len(got) != 1 || got[0].Rule != RuleCitation || !strings.Contains(got[0].Detail, "[3]") {
		t.Fatalf("got %+v", got)
	}
	if got := CheckCitations("见 [2]。", []int{1, 2}); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
