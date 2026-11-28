package assistant

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
)

//go:embed prompts/concept_summary.md
var conceptSummarySystem string

// SummaryPrompt is the prompt of the concept index's summary tree (#225):
// a chapter's summary from its lessons', the course's from its chapters'.
var SummaryPrompt = Prompt{Version: "concept-summary-1", System: strings.TrimSpace(conceptSummarySystem)}

// SummaryPart is a part of what a summary covers: a lesson of a chapter,
// or a chapter of the course, with its own summary.
type SummaryPart struct {
	Title   string
	Summary string
}

// SummaryInput is what a summary covers: a chapter of the course (Chapter
// set) or the whole course, and its parts.
type SummaryInput struct {
	CourseTitle string
	Chapter     string
	Parts       []SummaryPart
}

// Messages is the request of a summary.
func (in SummaryInput) Messages() []Message {
	var b strings.Builder
	b.WriteString(headCourseInfo + "\n课程：" + in.CourseTitle)
	if in.Chapter != "" {
		b.WriteString("\n\n## 要概括的\n一章：" + in.Chapter + "\n\n## 这一章的各讲")
	} else {
		b.WriteString("\n\n## 要概括的\n整门课程\n\n## 课程的各章")
	}
	for _, p := range in.Parts {
		b.WriteString("\n\n### " + p.Title + "\n" + strings.TrimSpace(p.Summary))
	}
	return []Message{{Role: RoleSystem, Content: SummaryPrompt.System}, {Role: RoleUser, Content: b.String()}}
}

// Summarize writes one summary of the summary tree on the turn's model,
// with no tools and no web: its reply, trimmed. An empty reply is a
// *RunError of CodeMalformedOutput. The meta is what it used, for the site
// to be charged, on failure too.
func Summarize(ctx context.Context, run Run, in SummaryInput) (string, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	run.Tools, run.Web = nil, nil
	run.Turn.ProviderWeb, run.Turn.Web = false, nil
	discard := Sink{Delta: func(Delta) error { return nil }, Activity: func(Activity) error { return nil }}
	r := newAnalysisRun(run, discard)
	r.plain = true
	c, err := r.loop(ctx, in.Messages())
	if err != nil {
		return "", r.meta(), err
	}
	text := strings.TrimSpace(c.raw)
	if text == "" {
		return "", r.meta(), &RunError{Code: CodeMalformedOutput, Cause: errors.New("the summary is empty")}
	}
	return text, r.meta(), nil
}
