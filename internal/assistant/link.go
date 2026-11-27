package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// ToolLinkCourse is the only tool of a run's linking step (ADR 0011), and
// the kind of the activity the reader sees while it goes.
const ToolLinkCourse = "link_course"

// LinkCandidate is a section of another lesson an answer may link to, as
// the linking step shows it to the model: its label, where it is in the
// course and its text.
type LinkCandidate struct {
	Label  string
	Lesson string
	// Section is "" for the top of the lesson
	Section string
	Text    string
}

// Linker finds the candidate sections in an answer (ADR 0011); none, and the run
// ends with the answer.
type Linker func(ctx context.Context, answer string) ([]LinkCandidate, error)

// CourseLink is a candidate the model linked: its place in the list the
// linker gave, and a sentence on how it helps the reader understand the
// answer.
type CourseLink struct {
	Candidate int
	Note      string
}

// Reply is what a follow-up run wrote: the answer, and the candidates it
// linked, if it linked any.
type Reply struct {
	Text  string
	Links []CourseLink
}

// maxCandidateText is how much of a candidate's section the linking step
// shows, in characters.
const maxCandidateText = 2000

const linkIntro = "此次作答提到了这些课程内容。判断哪些和当前回答真正相关：只是字面上相同、实际讲的不是一回事的，不算相关。" +
	"用 link_course 只列出真正相关的小节，每条附一句话，说明这条课程内容能怎样帮读者理解当前回答，不要写两者的异同；" +
	"判断为不相关的不要列出，都不相关时传空列表。"

var linkSpec = ToolSpec{
	Name:        ToolLinkCourse,
	Description: "列出和当前回答真正相关的课程内容：每一项是候选小节的标识和一句话说明；都不相关时 links 为空列表。",
	Parameters: json.RawMessage(`{"type":"object","properties":{"links":{"type":"array","items":{"type":"object",` +
		`"properties":{"section":{"type":"string","description":"候选小节的标识，如 s1"},"note":{"type":"string","description":"一句话说明这条课程内容能怎样帮读者理解当前回答"}},` +
		`"required":["section","note"],"additionalProperties":false}}},"required":["links"],"additionalProperties":false}`),
}

// CandidateRef is how the linking step names the candidate section at
// place i.
func CandidateRef(i int) string { return fmt.Sprintf("s%d", i+1) }

// linkMessage shows the candidates to the model.
func linkMessage(cs []LinkCandidate) string {
	var b strings.Builder
	b.WriteString(linkIntro)
	for i, c := range cs {
		where := "讲「" + c.Lesson + "」"
		if c.Section != "" {
			where += " › 小节「" + c.Section + "」"
		}
		text := strings.TrimSpace(c.Text)
		if r := []rune(text); len(r) > maxCandidateText {
			text = string(r[:maxCandidateText]) + "…"
		}
		fmt.Fprintf(&b, "\n\n[%s] %s\n位置：%s\n正文：\n%s", CandidateRef(i), c.Label, where, text)
	}
	return b.String()
}

// picked reads the links of a link_course call: those that name a
// candidate section, each once; nil when the call is not one.
func picked(calls []ToolCall, n int) []CourseLink {
	var out []CourseLink
	seen := map[int]bool{}
	for _, c := range calls {
		if c.Name != ToolLinkCourse {
			continue
		}
		var args struct {
			Links []struct {
				Section string `json:"section"`
				Note    string `json:"note"`
			} `json:"links"`
		}
		if err := json.Unmarshal([]byte(c.Arguments), &args); err != nil {
			continue
		}
		for _, l := range args.Links {
			for i := range n {
				if strings.TrimSpace(l.Section) == CandidateRef(i) && !seen[i] {
					seen[i] = true
					out = append(out, CourseLink{Candidate: i, Note: strings.TrimSpace(l.Note)})
				}
			}
		}
	}
	return out
}

// link is the linking step after an answer (ADR 0011): with candidates,
// one more call that sees them and may only call link_course. It never
// fails the run: the answer is whole, and what stops the step (the
// deadline, the credits, the model) only leaves it without links.
func (r *analysisRun) link(ctx context.Context, msgs []Message, answer called, find Linker) []CourseLink {
	if r.past(r.run.Deadline) {
		return nil
	}
	cs, err := find(ctx, answer.raw)
	if err != nil {
		slog.Error("course links not looked for", "err", err)
		return nil
	}
	if len(cs) == 0 {
		return nil
	}
	act := Activity{ID: "link_course-1", Kind: ToolLinkCourse, Status: ActivityRunning}
	if r.out.Activity(act) != nil {
		return nil
	}
	msgs = appendMessages(msgs,
		Message{Role: RoleAssistant, Content: answer.raw, Reasoning: answer.reasoning, Native: answer.result.Native},
		Message{Role: RoleUser, Content: linkMessage(cs)})
	// the step must call link_course; a thinking model may not be forced
	// to call a tool, so it is only asked to
	req := Request{Model: r.run.Turn.Model, Messages: msgs, Tools: []ToolSpec{linkSpec}, ToolChoice: "required", MaxTokens: maxTokens, Thinking: r.run.Turn.Thinking}
	if r.run.Turn.Thinking {
		req.ToolChoice, req.MaxTokens = "auto", maxTokensThinking
	}
	r.quiet = true
	c, err := r.call(ctx, req)
	r.quiet = false
	var links []CourseLink
	act.Status = ActivityDone
	if err != nil {
		act.Status = ActivityFailed
		slog.Info("course links stopped short", "err", err)
	} else {
		links = picked(c.toolCalls, len(cs))
	}
	_ = r.out.Activity(act)
	return links
}
