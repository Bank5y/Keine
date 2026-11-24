package assistant

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Length limits of an analysis, in characters. The concept explanation
// has the same limit as a note.
const (
	MaxTitle       = 100
	MaxConcept     = 300
	MaxExplanation = 20000
)

var (
	headingLine  = regexp.MustCompile(`(?m)^ {0,3}#{1,6}(\s|$)`)
	listLine     = regexp.MustCompile(`(?m)^ {0,3}([-*+]|\d+[.)])\s`)
	markdownLink = regexp.MustCompile(`\]\([^)]*\)|\]\[[^\]]*\]|^\[[^\]]+\]:\s`)
	bareURL      = regexp.MustCompile(`(?i)https?://|<a\s`)
	svgTag       = regexp.MustCompile(`(?i)<svg[\s>]|data:image/svg`)
	mermaidOpen  = regexp.MustCompile("(?m)^ {0,3}(```|~~~)\\s*mermaid\\s*$")
	// rawToolCall is the tool call markup some models write as text when a
	// request carries history with tool calls but no tools (#114).
	rawToolCall = regexp.MustCompile(`<｜[^｜]*tool[^｜]*｜>|<\|tool|tool▁call|<tool_call>|"tool_calls"\s*:`)
)

// mermaidKinds are the diagram types an analysis may draw.
var mermaidKinds = []string{"flowchart TD", "sequenceDiagram", "stateDiagram-v2"}

// checkAnalysis lists what is wrong with a finished answer, in words the
// model is shown when asked to answer again; none means it is valid.
func checkAnalysis(raw string, p *tagParser) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if rawToolCall.MatchString(raw) {
		add("答复里有原始的工具调用标记")
	}
	for _, s := range allParts {
		if !p.closed[s] {
			add("缺少完整的 %s…%s", openTag(s), closeTag(s))
		}
	}
	if p.repeated {
		add("有一段标签写了不止一次")
	}
	a := p.parts()
	if p.closed[PartTitle] && a.Title == "" {
		add("标题是空的")
	}
	if p.closed[PartConcept] && a.Concept == "" {
		add("概念是空的")
	}
	if headingLine.MatchString(a.Concept) || listLine.MatchString(a.Concept) {
		add("概念里不能有标题或列表")
	}
	if headingLine.MatchString(a.Explanation) {
		add("概念解释里不能写 # 标题")
	}
	if markdownLink.MatchString(a.Explanation) || bareURL.MatchString(a.Explanation) {
		add("概念解释里不能有链接")
	}
	if svgTag.MatchString(a.Explanation) {
		add("不能直接写 SVG")
	}
	problems = append(problems, checkMermaid(a.Explanation)...)
	if n := utf8.RuneCountInString(a.Title); n > MaxTitle {
		add("标题 %d 字，超过上限 %d 字", n, MaxTitle)
	}
	if n := utf8.RuneCountInString(a.Concept); n > MaxConcept {
		add("概念 %d 字，超过上限 %d 字", n, MaxConcept)
	}
	if n := utf8.RuneCountInString(a.Explanation); n > MaxExplanation {
		add("概念解释 %d 字，超过上限 %d 字", n, MaxExplanation)
	}
	return problems
}

// checkMermaid allows at most one diagram, of a vertical kind.
func checkMermaid(body string) []string {
	opens := mermaidOpen.FindAllStringIndex(body, -1)
	var problems []string
	if len(opens) > 1 {
		problems = append(problems, "最多只能有一张 Mermaid 图")
	}
	for _, o := range opens {
		first := ""
		for _, line := range strings.Split(body[o[1]:], "\n") {
			if first = strings.TrimSpace(line); first != "" {
				break
			}
		}
		ok := false
		for _, k := range mermaidKinds {
			if first == k || strings.HasPrefix(first, k+" ") {
				ok = true
			}
		}
		if !ok {
			problems = append(problems, "Mermaid 图只能是 flowchart TD、sequenceDiagram 或 stateDiagram-v2")
		}
	}
	return problems
}

// CheckAnalysis reads a finished answer as the loop does and checks it
// with the server's rules: the three parts, and what is wrong with it
// (none when it is valid). keine-eval checks recorded answers with it.
func CheckAnalysis(raw string) (Analysis, []string) {
	p := newTagParser(func(Part, string) error { return nil })
	_ = p.feed(raw)
	return p.parts(), checkAnalysis(raw, p)
}

// CheckReply lists what is wrong with a reply of the dialogue by what the
// follow-up prompt asks of it (#141): Markdown with no link (the sources
// are recorded by the system), no SVG, no raw tool call markup, at most
// one diagram of an allowed kind. The loop takes a reply as it is written;
// keine-eval judges replies by these rules.
func CheckReply(md string) []string {
	var problems []string
	if rawToolCall.MatchString(md) {
		problems = append(problems, "回复里有原始的工具调用标记")
	}
	if markdownLink.MatchString(md) || bareURL.MatchString(md) {
		problems = append(problems, "回复里不能有链接")
	}
	if svgTag.MatchString(md) {
		problems = append(problems, "不能直接写 SVG")
	}
	return append(problems, checkMermaid(md)...)
}

// MermaidBlocks are the Mermaid diagrams of a Markdown text, their source
// without the fences.
func MermaidBlocks(md string) []string {
	var out []string
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		m := mermaidOpen.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		var src []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != m[1]; i++ {
			src = append(src, lines[i])
		}
		out = append(out, strings.Join(src, "\n"))
	}
	return out
}
