package assistant

import (
	"context"
	_ "embed"
	"slices"
	"strings"
	"time"
)

//go:embed prompts/followup.md
var followupSystem string

// FollowupPrompt is the prompt of a turn after the analysis (#136). Its
// context is not a passage but the exploration (FollowupContext).
var FollowupPrompt = Prompt{
	Version: "followup-1",
	System:  strings.TrimSpace(followupSystem),
}

// Doc is an exploration document as the model reads it: its sections in
// their fixed order, the ones after the meaning only when they have items.
type Doc struct {
	Title, Intro, Meaning                         string
	Footnotes, Extensions, Expansions, References []string
}

func (d Doc) markdown() string {
	var b strings.Builder
	b.WriteString("【当前探索文档】\n\n# " + d.Title + "\n\n> " + d.Intro + "\n\n## 含义\n\n" + strings.TrimSpace(d.Meaning))
	for _, s := range []struct {
		name  string
		items []string
	}{{"脚注", d.Footnotes}, {"延伸", d.Extensions}, {"扩展", d.Expansions}, {"参考", d.References}} {
		if len(s.items) == 0 {
			continue
		}
		b.WriteString("\n\n## " + s.name + "\n")
		for _, it := range s.items {
			b.WriteString("\n- " + it)
		}
	}
	return b.String()
}

// Asked is a question of the dialogue: its text and the passages the
// reader selected for it.
type Asked struct {
	Text      string
	Fragments []string
}

func (q Asked) content() string {
	var parts []string
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

// FollowupContext is what a turn after the analysis reads (#94, ADR 0009):
// the request the analysis ran on as it was when pinned (its snapshot),
// the analysis's reply, the document as it is now, and the turns along the
// node's chain of ancestors, up to where it is asked.
type FollowupContext struct {
	// Snapshot is the analysis's request; its system prompt gives way to
	// the follow-up's
	Snapshot   []Message
	FirstReply string
	Doc        Doc
	History    []Exchange
}

// Messages puts a question after its context. Everything before the
// question is the same for every turn of a chain (until a sync changes
// the document), so the start of the request can be cached.
func (c FollowupContext) Messages(q Asked) []Message {
	msgs := []Message{{Role: RoleSystem, Content: FollowupPrompt.System}}
	for _, m := range c.Snapshot {
		if m.Role != RoleSystem {
			msgs = append(msgs, m)
		}
	}
	msgs = append(msgs,
		Message{Role: RoleAssistant, Content: c.FirstReply},
		Message{Role: RoleUser, Content: c.Doc.markdown()})
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
