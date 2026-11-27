package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// dialogueReport is a run of one follow-up case: a turn in the main node,
// then a child node whose first turn linked lesson 05 and broke a rule.
func dialogueReport(prompt, word string) Report {
	r := report(prompt, word)
	r.Followup = "followup-3"
	c := Case{ID: "tcp-dialogue", Category: "系统与网络", Kind: "child", Quote: "TCP",
		Turns: []Turn{{Ask: "TCP 为什么要三次握手？"}, {Fork: course.NodeChild, Ask: "小数据包怎么传？"}}}
	r.Cases = append(r.Cases, c)
	for attempt := 1; attempt <= 2; attempt++ {
		res := Result{Case: c.ID, Attempt: attempt, Failures: []Failure{}, Stats: Stats{Steps: 1, FirstTextMs: 500},
			Turns: []TurnResult{
				{Ask: c.Turns[0].Ask, Reply: word + "握手回复", Stats: Stats{Steps: 2, ToolCalls: 1, DurationMs: 4000, FirstTextMs: 800, Usage: usage(2000, 400), Credits: 0.02}},
				{Fork: course.NodeChild, Ask: c.Turns[1].Ask, Reply: word + "小包回复", Stats: Stats{DurationMs: 6000, FirstTextMs: 700, Usage: usage(3000, 500), Credits: 0.03},
					Linking: &Linking{Index: course.ConceptIndexBuilt,
						Candidates: []LinkEntry{{Label: "Nagle 算法", Lesson: "05｜动态数据传输", Section: "小包"}, {Label: "滑动窗口", Lesson: "04｜可靠传输"}},
						Chosen:     []LinkEntry{{Label: "Nagle 算法", Lesson: "05｜动态数据传输", Section: "小包", Note: word + "攒小包再发"}}}},
			}}
		if attempt == 1 {
			res.Failures = []Failure{{Turn: 2, Rule: RuleNarration, Detail: "出现了「本文档」"}}
		}
		r.Results = append(r.Results, res)
	}
	return r
}

func usage(in, out int64) assistant.Usage { return assistant.Usage{Input: n(in), Output: n(out)} }

func TestTheBaselineKeepsTheTurnsAndTheirCourseLinksButNoReply(t *testing.T) {
	b, err := json.Marshal(dialogueReport("analysis-4", "新版").Baseline())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, text := range []string{"新版", "握手", "小数据包", "攒小包"} {
		if strings.Contains(s, text) {
			t.Fatalf("baseline has text %q: %s", text, s)
		}
	}
	for _, want := range []string{`"followup":"followup-3"`, `"fork":"child"`, `"index":"built"`, `"candidates":["Nagle 算法","滑动窗口"]`, `"chosen":["Nagle 算法"]`, `"broken":["` + RuleNarration + `"]`, `"turnTotals"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("baseline lacks %s: %s", want, s)
		}
	}
	// the analysis of the case broke nothing: the turn did
	var got Baseline
	_ = json.Unmarshal(b, &got)
	run := got.Cases[2].Runs[0]
	if len(run.Broken) != 0 || len(run.Turns) != 2 || len(run.Turns[1].Broken) != 1 || got.HardFailures != 2 {
		t.Fatalf("baseline run: %+v, hard failures %d", run, got.HardFailures)
	}
}

func TestTheReportListsEachTurnWithWhatItLinked(t *testing.T) {
	md := dialogueReport("analysis-4", "新版").Markdown()
	for _, want := range []string{"## 后续轮", "| ❌ tcp-dialogue | 1 | 2 | 子节点 |", "| ✅ tcp-dialogue | 1 | 1 | 主节点 |",
		"Nagle 算法（05｜动态数据传输 › 小包）", "✓ Nagle 算法：新版攒小包再发", "tcp-dialogue 第 1 次第 2 轮"} {
		if !strings.Contains(md, want) {
			t.Fatalf("report lacks %q:\n%s", want, md)
		}
	}
}

func TestTheBlindPageComparesTheRepliesAndTheirCourseLinks(t *testing.T) {
	page := string(BlindHTML(dialogueReport("analysis-5", "新版"), ptr(dialogueReport("analysis-4", "旧版")), 3, ""))
	for _, want := range []string{"新版握手回复", "旧版握手回复", "新版小包回复", "旧版小包回复", "小数据包怎么传？", "子节点", "课程里的相关内容", "新版攒小包再发", "旧版攒小包再发", "滑动窗口"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
}

func TestTheBlindPageRendersTheMarkdownOfAnswersAndReplies(t *testing.T) {
	r := dialogueReport("analysis-5", "新版")
	r.Results[0].Analysis.Explanation = "**任期**是编号。\n\n```mermaid\nflowchart TD\n  A --> B\n```"
	r.Results[len(r.Results)-1].Turns[0].Reply = "## 区别\n\n| 项 | 说明 |\n|---|---|\n| Nagle | 攒小包 |\n\n<script>alert(1)</script>"
	page := string(BlindHTML(r, nil, 1, "../../web/node_modules/mermaid/dist/mermaid.min.js"))
	for _, want := range []string{"<strong>任期</strong>", `<code class="language-mermaid">flowchart TD`, "<h2>区别</h2>", "<td>Nagle</td>", `src="../../web/node_modules/mermaid/dist/mermaid.min.js"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Fatal("raw HTML in a reply is put on the page")
	}
}
