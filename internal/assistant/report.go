package assistant

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed prompts/writeup.md
var reportSystem string

// ReportPrompt is the prompt of an exploration report (ADR 0012): it reads
// the whole dialogue tree, not a passage.
var ReportPrompt = newPrompt("report-1", reportSystem, DefaultStyle)

// The parts of a report besides the title.
const (
	PartSummary Part = "summary"
	PartBody    Part = "body"
	PartPoints  Part = "points"
)

// Report is a valid report as the model writes it: the references are not
// the model's but the server's, from the turns' source lists.
type Report struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
	Points  string `json:"points"`
}

// output tokens a report may write, with thinking off and on
const (
	reportMaxTokens         = 8192
	reportMaxTokensThinking = 12288
)

const reportRemedy = "上一次答复不合格：%s。请只按 <rep_title>/<rep_summary>/<rep_body>/<rep_points> 格式重新作答，标签外不要写任何文字。"

var reportForm = &tagForm{prefix: "rep_", parts: []Part{PartTitle, PartSummary, PartBody, PartPoints}, remedy: reportRemedy,
	maxTokens: reportMaxTokens, maxThinking: reportMaxTokensThinking}

func init() { reportForm.check = checkReport }

// checkReport lists what is wrong with a finished report.
func checkReport(raw string, p *tagParser) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if rawToolCall.MatchString(raw) {
		add("答复里有原始的工具调用标记")
	}
	names := map[Part]string{PartTitle: "标题", PartSummary: "摘要", PartBody: "正文", PartPoints: "要点"}
	for _, s := range reportForm.parts {
		switch {
		case !p.closed[s]:
			add("缺少完整的 %s…%s", reportForm.open(s), reportForm.close(s))
		case strings.TrimSpace(p.get(s)) == "":
			add("%s是空的", names[s])
		}
	}
	if p.repeated {
		add("有一段标签写了不止一次")
	}
	title, summary := p.get(PartTitle), p.get(PartSummary)
	if headingLine.MatchString(title) || listLine.MatchString(title) || strings.Contains(title, "\n") {
		add("标题只能是一行文字")
	}
	if headingLine.MatchString(summary) || listLine.MatchString(summary) {
		add("摘要里不能有标题或列表")
	}
	if svgTag.MatchString(p.get(PartBody)) {
		add("不能直接写 SVG")
	}
	if n := utf8.RuneCountInString(title); n > MaxTitle {
		add("标题 %d 字，超过上限 %d 字", n, MaxTitle)
	}
	return problems
}

// ReportInput is how many tokens a report's request may hold on the
// turn's model: its window less what the report may write.
func ReportInput(t Turn) int {
	window := t.ContextWindow
	if window <= 0 {
		window = DefaultContextWindow
	}
	out := reportMaxTokens
	if t.Thinking {
		out = reportMaxTokensThinking
	}
	return max(window-out, 0)
}

// ReportNode is a node of the dialogue as a report reads it. Parent is the
// index of the node it forked from among the nodes given (-1 for the main
// node), Fork the turn of the parent it forked at (1-based, counting the
// analysis).
type ReportNode struct {
	Kind   string
	Name   string
	Parent int
	Fork   int
	Turns  []Exchange
}

// ReportContext is what a report reads (ADR 0012): the background as a
// follow-up reads it, then every node of the dialogue tree with all its
// turns. The main node, first, starts with the first exchange.
type ReportContext struct {
	Snapshot      []Message
	Title         string
	FirstQuestion string
	// FirstReply is the analysis's reply as kept, in its tags
	FirstReply string
	Nodes      []ReportNode
}

var nodeKindNames = map[string]string{"main": "主节点", "child": "子节点", "branch": "分支节点", "divergent": "发散节点"}

// Messages is a report's request.
func (c ReportContext) Messages() []Message {
	var b strings.Builder
	fmt.Fprintf(&b, "以下是读者的整棵探索对话，探索的标题是「%s」。每个节点列出它的所有轮次。\n", c.Title)
	for i, n := range c.Nodes {
		kind := nodeKindNames[n.Kind]
		if n.Parent < 0 {
			fmt.Fprintf(&b, "\n## 节点 %d：%s\n", i+1, kind)
		} else {
			fmt.Fprintf(&b, "\n## 节点 %d：%s「%s」（从节点 %d 的第 %d 轮岔出）\n", i+1, kind, n.Name, n.Parent+1, n.Fork)
		}
		turns := n.Turns
		if n.Parent < 0 {
			turns = append([]Exchange{{Asked: Asked{Text: c.FirstQuestion}, Reply: analysisMarkdown(c.FirstReply)}}, turns...)
		}
		for j, t := range turns {
			fmt.Fprintf(&b, "\n### 第 %d 轮\n\n【读者】\n%s\n\n【助手】\n%s\n", j+1, t.Asked.content(), strings.TrimSpace(t.Reply))
		}
	}
	b.WriteString("\n请根据以上整棵探索对话写这份探索报告。")
	return []Message{
		{Role: RoleSystem, Content: ReportPrompt.System},
		{Role: RoleUser, Content: background(c.Snapshot)},
		{Role: RoleUser, Content: b.String()},
	}
}

// WriteReport writes a report on the turn's model: the analysis's loop
// with no tools, its four parts streaming to out as they are written. It
// returns the valid report, a *RunError, or the context's error.
func WriteReport(ctx context.Context, run Run, msgs []Message, out Sink) (Report, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	run.Tools, run.Web, run.Turn.ProviderWeb = nil, nil, false
	r := newAnalysisRun(run, out)
	r.form = reportForm
	c, err := r.loop(ctx, msgs)
	if err != nil {
		return Report{}, r.meta(), err
	}
	p := c.parser
	return Report{Title: p.get(PartTitle), Summary: p.get(PartSummary), Body: p.get(PartBody), Points: p.get(PartPoints)}, r.meta(), nil
}
