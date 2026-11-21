package assistant

import (
	"context"
	_ "embed"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed prompts/followup.md
var followupSystem string

// FollowupPrompt is the prompt of a turn after the analysis (#136, ADR
// 0011): the same for every kind of node. Its context is not a passage but
// the exploration (FollowupContext).
var FollowupPrompt = newPrompt("followup-2", followupSystem, DefaultStyle)

// FirstQuestion is the question an exploration's analysis stands for
// (ADR 0011): to explain the term (by the exploration's title, which keeps
// how the text writes it), or, when the reader selected a sentence, the
// sentence itself.
func FirstQuestion(quote, title string) string {
	if isTerm(quote) {
		return "解释「" + title + "」的含义"
	}
	return "解释这段话：「" + quote + "」"
}

// termMaxRunes is the longest a selection can be and still be a term.
const termMaxRunes = 24

// isTerm tells a selected term (a concept's name, an abbreviation, a proper
// noun) from a sentence: a term is short and has no punctuation that ends
// or splits a clause.
func isTerm(quote string) bool {
	q := strings.TrimSpace(quote)
	if q == "" || utf8.RuneCountInString(q) > termMaxRunes {
		return false
	}
	if strings.ContainsAny(q, "。！？；，：、!?;,:\n") {
		return false
	}
	// a period ends an English sentence, not an abbreviation like "e.g"
	return !strings.HasSuffix(q, ".") && !strings.Contains(q, ". ")
}

// legacyTags reads an analysis kept before the concept explanation (#157)
// in the tags of now: its intro was the concept, its meaning ("body") the
// concept explanation.
var legacyTags = strings.NewReplacer(
	"<exp_intro>", openTag(PartConcept), "</exp_intro>", closeTag(PartConcept),
	"<exp_body>", openTag(PartExplanation), "</exp_body>", closeTag(PartExplanation))

// analysisMarkdown is the analysis's reply as the reader saw it: its tags
// taken out (ADR 0011). A reply without them stays as it is.
func analysisMarkdown(reply string) string {
	p := newTagParser(func(Part, string) error { return nil })
	_ = p.feed(legacyTags.Replace(reply))
	a := p.parts()
	if a.Title == "" && a.Concept == "" && a.Explanation == "" {
		return strings.TrimSpace(reply)
	}
	var parts []string
	if a.Title != "" {
		parts = append(parts, "# "+a.Title)
	}
	for _, s := range []string{a.Concept, a.Explanation} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// backgroundNote heads the background: what the reader has read.
const backgroundNote = "以下是读者已经读过的背景。"

// background is the part of the analysis's request a follow-up keeps (ADR
// 0011): the course info and the selected text, with the note that the
// reader has read them. The outline and the lesson text are left for the
// tools. The snapshot itself is not changed.
func background(snapshot []Message) string {
	var content string
	for _, m := range snapshot {
		if m.Role == RoleUser {
			content = m.Content
			break
		}
	}
	var kept []string
	for _, s := range sections(content) {
		if strings.HasPrefix(s, headCourseInfo) || strings.HasPrefix(s, headSelection) {
			kept = append(kept, s)
		}
	}
	return backgroundNote + "\n\n" + strings.Join(kept, "\n\n")
}

// sectionHeads are the sources' headings in the order AnalysisPrompt
// puts them; the lesson comes whole or as its sections.
var sectionHeads = []string{headCourseInfo, headOutline, headSelection, headLesson, headLessonSections}

// sections splits an analysis's request into its sources, in the order
// AnalysisPrompt puts them: each source begins at the first of the heads
// still to come, so a heading inside the lesson text does not split it.
func sections(content string) []string {
	var starts []int
	from := 0
	for _, h := range sectionHeads {
		i := -1
		if strings.HasPrefix(content[from:], h) {
			i = from
		} else if j := strings.Index(content[from:], "\n\n"+h); j >= 0 {
			i = from + j + 2
		}
		if i < 0 {
			continue
		}
		starts = append(starts, i)
		from = i + len(h)
	}
	var out []string
	for k, i := range starts {
		end := len(content)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		out = append(out, strings.TrimSpace(content[i:end]))
	}
	return out
}

// nodeNotes say which way to answer the turn a node of each kind forks
// with (ADR 0011), by the node's kind; they bind only this answer.
var nodeNotes = map[string]string{
	"child":     "此次作答沿上面讨论的这一点往深处讲：机制、原因、细节和边界情况。",
	"branch":    "此次作答从上面讨论的概念横向展开：讲和它相关的概念，以及它们之间的关系。",
	"divergent": "此次作答跳出当前主题：讲同类的做法和其他领域的类比，并和这里的做法对照；需要时用网络搜索。",
}

// NodeNote is the note the turn a node of kind forks with begins with; ""
// for the main node, whose turns carry none.
func NodeNote(kind string) string { return nodeNotes[kind] }

// Asked is a question of the dialogue: its text, the passages the reader
// selected for it, and, on the turn a node forks with, the node's note.
type Asked struct {
	Text      string
	Fragments []string
	// Note heads the question; the reader does not see it
	Note string
}

func (q Asked) content() string {
	var parts []string
	if q.Note != "" {
		parts = append(parts, q.Note)
	}
	for _, f := range q.Fragments {
		parts = append(parts, "【选中片段】\n「"+f+"」")
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, "\n\n")
}

// Exchange is an earlier turn: what was asked and the reply.
type Exchange struct {
	Asked Asked
	Reply string
}

// FollowupContext is what a turn after the analysis reads (ADR 0009,
// ADR 0011): the background kept from the request the analysis ran on as it
// was when pinned (its snapshot), the first exchange (the question the
// analysis stands for and its reply), and the turns along the node's chain
// of ancestors, up to where it is asked.
type FollowupContext struct {
	// Snapshot is the analysis's request; only its course info and selected
	// text are read
	Snapshot      []Message
	FirstQuestion string
	// FirstReply is the analysis's reply as kept, in its tags
	FirstReply string
	History    []Exchange
}

// Messages puts a question after its context. Everything before the
// question is the same for every turn of a chain, so a turn's request
// starts with the one before it and its reply, and can be cached.
func (c FollowupContext) Messages(q Asked) []Message {
	msgs := []Message{
		{Role: RoleSystem, Content: FollowupPrompt.System},
		{Role: RoleUser, Content: background(c.Snapshot)},
		{Role: RoleUser, Content: c.FirstQuestion},
		{Role: RoleAssistant, Content: analysisMarkdown(c.FirstReply)},
	}
	for _, e := range c.History {
		msgs = append(msgs, Message{Role: RoleUser, Content: e.Asked.content()}, Message{Role: RoleAssistant, Content: e.Reply})
	}
	return append(msgs, Message{Role: RoleUser, Content: q.content()})
}

// Followup runs a turn after the analysis on the turn's model: the same
// loop, tools and limits as an analysis (ADR 0008), but the reply is
// Markdown that streams to out as it is written. It returns the reply; on
// a *RunError or a cancelled context, what of it the reader already saw.
func Followup(ctx context.Context, run Run, msgs []Message, out Sink) (string, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	var web *Web
	if run.Web != nil {
		web = newWeb(*run.Web)
		run.Tools = append(slices.Clip(run.Tools), web.Tools()...)
	}
	r := newAnalysisRun(run, out)
	r.web, r.plain = web, true
	c, err := r.loop(ctx, msgs)
	if err != nil {
		return r.text.String(), r.meta(), err
	}
	return c.raw, r.meta(), nil
}
