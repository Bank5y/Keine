package eval

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/Bank5y/keine/internal/assistant"
)

// The hard rules: an analysis that fails any of them is a hard failure,
// of which there must be none.
const (
	// RuleServer: the server's own check (three parts, no heading, no
	// link, one diagram of an allowed kind, no raw tool call markup…).
	RuleServer = "server_check"
	// RuleMermaid: a diagram mermaid's own parser cannot parse.
	RuleMermaid = "mermaid_parses"
	// RuleLanguage: the title is not in the language of the lesson.
	RuleLanguage       = "title_language"
	RuleMustInclude    = "must_include"
	RuleMustNotInclude = "must_not_include"
	// RuleCase: the case no longer matches the course (the course, lesson
	// or selection is gone, or the course has a newer version), so stale
	// cases get rewritten.
	RuleCase = "case_valid"
	// RuleRun: the run did not end with an answer (the model failing, a
	// timeout).
	RuleRun = "run_completes"
)

// Failure is a hard rule an analysis broke, and how.
type Failure struct {
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

// MermaidParser parses diagrams with mermaid's own parser: an error for
// each diagram that does not parse (nil for one that does), or err when
// the parser could not be run.
type MermaidParser interface {
	Parse(diagrams []string) (errs []error, err error)
}

// Check judges a finished answer by the hard rules: raw is what the model
// wrote, lesson the lesson's text.
func Check(raw string, c Case, lesson string, mermaid MermaidParser) []Failure {
	var out []Failure
	a, problems := assistant.CheckAnalysis(raw)
	for _, p := range problems {
		out = append(out, Failure{Rule: RuleServer, Detail: p})
	}
	if diagrams := assistant.MermaidBlocks(a.Explanation); len(diagrams) > 0 {
		errs, err := mermaid.Parse(diagrams)
		if err != nil {
			out = append(out, Failure{Rule: RuleMermaid, Detail: "mermaid 的解析器没有运行: " + err.Error()})
		}
		for i, e := range errs {
			if e != nil {
				out = append(out, Failure{Rule: RuleMermaid, Detail: fmt.Sprintf("第 %d 张图: %s", i+1, firstLine(e.Error()))})
			}
		}
	}
	if title := strings.TrimSpace(a.Title); title != "" && !titleFollows(title, c.Quote, lesson) {
		out = append(out, Failure{Rule: RuleLanguage, Detail: fmt.Sprintf("正文是%s, 标题不是", languageName(chinese(prose(lesson))))})
	}
	text := strings.ToLower(a.Title + "\n" + a.Concept + "\n" + a.Explanation)
	for _, k := range c.MustInclude {
		if !strings.Contains(text, strings.ToLower(k)) {
			out = append(out, Failure{Rule: RuleMustInclude, Detail: "没有「" + k + "」"})
		}
	}
	for _, k := range c.MustNotInclude {
		if strings.Contains(text, strings.ToLower(k)) {
			out = append(out, Failure{Rule: RuleMustNotInclude, Detail: "出现了「" + k + "」"})
		}
	}
	return out
}

// chinese tells whether a text is mostly Chinese: at least a third of its
// letters are Han.
func chinese(s string) bool {
	han, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
			letters++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return letters > 0 && han*3 >= letters
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

var inlineCode = regexp.MustCompile("`[^`\\n]*`")

// prose is a lesson's text without its code, whose language is not the
// lesson's.
func prose(md string) string {
	var b strings.Builder
	fence := ""
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case strings.HasPrefix(t, "```"), strings.HasPrefix(t, "~~~"):
			fence = t[:3]
		default:
			b.WriteString(inlineCode.ReplaceAllString(line, ""))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// titleFollows tells whether a title is in the lesson's language, which
// its prose tells. In a Chinese lesson a title with no Han may only be the
// selected term as it is written; in any other, a title has no Han.
func titleFollows(title, quote, lesson string) bool {
	if chinese(prose(lesson)) {
		return hasHan(title) || strings.EqualFold(strings.TrimSpace(quote), title)
	}
	return !hasHan(title)
}

func languageName(zh bool) string {
	if zh {
		return "中文"
	}
	return "外文"
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}
