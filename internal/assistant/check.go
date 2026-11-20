package assistant

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Length limits of an analysis, in characters. The meaning has the same
// limit as a note.
const (
	MaxTitle = 100
	MaxIntro = 300
	MaxBody  = 20000
)

var (
	headingLine  = regexp.MustCompile(`(?m)^ {0,3}#{1,6}(\s|$)`)
	listLine     = regexp.MustCompile(`(?m)^ {0,3}([-*+]|\d+[.)])\s`)
	boldLine     = regexp.MustCompile(`(?m)^ {0,3}(\*\*|__)[^\n]+(\*\*|__)[:：]?\s*$`)
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
	if p.closed[PartIntro] && a.Intro == "" {
		add("简介是空的")
	}
	if headingLine.MatchString(a.Intro) || listLine.MatchString(a.Intro) {
		add("简介里不能有标题或列表")
	}
	if headingLine.MatchString(a.Body) {
		add("含义里不能写标题")
	}
	if boldLine.MatchString(a.Body) {
		add("含义里不能用单独一行的加粗文字充当小标题")
	}
	if markdownLink.MatchString(a.Body) || bareURL.MatchString(a.Body) {
		add("含义里不能有链接")
	}
	if svgTag.MatchString(a.Body) {
		add("不能直接写 SVG")
	}
	problems = append(problems, checkMermaid(a.Body)...)
	if n := utf8.RuneCountInString(a.Title); n > MaxTitle {
		add("标题 %d 字，超过上限 %d 字", n, MaxTitle)
	}
	if n := utf8.RuneCountInString(a.Intro); n > MaxIntro {
		add("简介 %d 字，超过上限 %d 字", n, MaxIntro)
	}
	if n := utf8.RuneCountInString(a.Body); n > MaxBody {
		add("含义 %d 字，超过上限 %d 字", n, MaxBody)
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
