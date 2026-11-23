package eval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/assistant"
)

func n(v int64) *int64 { return &v }

// report is a run of two cases, twice each, on prompt; the second case's
// first run broke a hard rule.
func report(prompt, word string) Report {
	r := Report{Prompt: prompt, Model: "Flash", Started: time.Date(2026, 11, 22, 9, 30, 0, 0, time.UTC), Runs: 2,
		Cases: []Case{{ID: "raft-term", Category: "分布式系统", Kind: "term", Quote: "任期"}, {ID: "http-header", Category: "分布式系统", Kind: "sentence", Quote: "报文"}}}
	for _, c := range r.Cases {
		for attempt := 1; attempt <= 2; attempt++ {
			res := Result{Case: c.ID, Attempt: attempt, Failures: []Failure{},
				Raw:      "<exp_title>" + word + c.ID + "</exp_title>",
				Analysis: assistant.Analysis{Title: word + "标题" + c.ID, Concept: word + "概念", Explanation: word + "解释正文"},
				Stats:    Stats{Steps: 1, ToolCalls: 2, DurationMs: 3000, FirstTextMs: 900, Usage: assistant.Usage{Input: n(1000), Output: n(300)}, Credits: 0.01},
				Sources:  []assistant.WebSource{{Title: word + "资料", URL: "https://example.com/" + word}},
			}
			if c.ID == "http-header" && attempt == 1 {
				res.Failures = []Failure{{Rule: RuleServer, Detail: "概念解释里不能有 # 标题"}}
			}
			r.Results = append(r.Results, res)
		}
	}
	return r
}

func TestTheBaselineKeepsFiguresAndRulesButNoText(t *testing.T) {
	b, err := json.Marshal(report("analysis-4", "新版").Baseline())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, text := range []string{"新版", "<exp_", "example.com", "任期", "报文"} {
		if strings.Contains(s, text) {
			t.Fatalf("baseline has text %q: %s", text, s)
		}
	}
	for _, want := range []string{`"prompt":"analysis-4"`, `"raft-term"`, RuleServer, `"durationMs"`, `"hardFailures":1`} {
		if !strings.Contains(s, want) {
			t.Fatalf("baseline lacks %s: %s", want, s)
		}
	}
}

func TestAHardFailureFailsTheCommandAndIsMarkedInTheReport(t *testing.T) {
	r := report("analysis-4", "新版")
	if r.HardFailures() != 1 {
		t.Fatalf("hard failures = %d", r.HardFailures())
	}
	md := r.Markdown()
	if !strings.Contains(md, "硬失败：1") || !strings.Contains(md, "❌ http-header") || !strings.Contains(md, "概念解释里不能有 # 标题") || !strings.Contains(md, "✅ raft-term") {
		t.Fatalf("report:\n%s", md)
	}
	clean := report("analysis-4", "新版")
	for i := range clean.Results {
		clean.Results[i].Failures = []Failure{}
	}
	if clean.HardFailures() != 0 {
		t.Fatal("a clean run has hard failures")
	}
}

func TestTheBlindPageShowsBothVersionsShuffledUntilRevealed(t *testing.T) {
	page := string(BlindHTML(report("analysis-5", "新版"), ptr(report("analysis-4", "旧版")), 7))
	for _, want := range []string{"新版标题raft-term", "旧版标题raft-term", "新版标题http-header", "旧版标题http-header", "揭示版本"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	// the versions are named only inside what the reveal shows
	for _, name := range []string{"analysis-5", "analysis-4"} {
		if strings.Count(page, name) != strings.Count(page, `class="version">`+name) {
			t.Fatalf("%s is named outside the revealed labels", name)
		}
	}
	if !strings.Contains(page, ".version { display: none") || !strings.Contains(page, "body.revealed .version") {
		t.Fatal("labels are not hidden until revealed")
	}
	// the new version is on the left for some cases, on the right for others
	left := func(id string) bool {
		i, j := strings.Index(page, "新版标题"+id), strings.Index(page, "旧版标题"+id)
		return i < j
	}
	sides := map[bool]bool{}
	for seed := int64(0); seed < 8; seed++ {
		page = string(BlindHTML(report("analysis-5", "新版"), ptr(report("analysis-4", "旧版")), seed))
		sides[left("raft-term")] = true
	}
	if len(sides) != 2 {
		t.Fatal("the order is never shuffled")
	}
}

func TestTheBlindPageOfAFirstRunShowsItAlone(t *testing.T) {
	page := string(BlindHTML(report("analysis-5", "新版"), nil, 1))
	if !strings.Contains(page, "新版标题raft-term") || strings.Contains(page, "旧版") {
		t.Fatal("first run page")
	}
}

func ptr[T any](v T) *T { return &v }
