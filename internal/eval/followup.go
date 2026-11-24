package eval

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/lessonctx"
)

// TurnResult is a turn of a follow-up case as it was asked: the reply in
// full, the course links of the turn a node forks with, and its figures.
type TurnResult struct {
	Fork  course.NodeKind `json:"fork,omitempty"`
	Ask   string          `json:"ask"`
	Reply string          `json:"reply"`
	// Error is the run's error code, "" when it answered.
	Error   string                `json:"error,omitempty"`
	Linking *Linking              `json:"linking,omitempty"`
	Stats   Stats                 `json:"stats"`
	Sources []assistant.WebSource `json:"sources"`
}

// Linking is what the turn a node forks with linked of the course (ADR
// 0011): the status of the version's concept index, the entries the answer
// named that were offered to the model, and those it chose.
type Linking struct {
	Index      course.ConceptIndexStatus `json:"index"`
	Candidates []LinkEntry               `json:"candidates"`
	Chosen     []LinkEntry               `json:"chosen"`
}

// LinkEntry is an entry of the concept index: the concept, its lesson and
// section, and, once chosen, the model's sentence on it.
type LinkEntry struct {
	Concept string `json:"concept"`
	Lesson  string `json:"lesson"`
	Section string `json:"section,omitempty"`
	Note    string `json:"note,omitempty"`
}

func linkEntry(concept string, lesson course.EntryLesson, section *course.EntrySection, note string) LinkEntry {
	e := LinkEntry{Concept: concept, Lesson: lesson.Title, Note: note}
	if section != nil {
		e.Section = section.Title
	}
	return e
}

// dialogue asks a follow-up case's turns after its pinned analysis, the
// way the reader's dialogue asks them (ADR 0011): each turn reads the
// turns before it on its chain, and the turn a node forks with is linked
// to the course by the version's concept index. The chain stops at a turn
// that does not end with an answer.
func (r *Runner) dialogue(ctx context.Context, res *Result, c Case, pinned assistant.FollowupContext, lib *lessonctx.Library) error {
	pick := r.Followup
	if pick == nil {
		pick = r.Turn
	}
	turn, err := pick(ctx)
	if err != nil {
		return err
	}
	for i, t := range c.Turns {
		n := i + 1
		fail := func(rule, detail string) {
			res.Failures = append(res.Failures, Failure{Turn: n, Rule: rule, Detail: detail})
		}
		q := assistant.Asked{Text: t.Ask}
		tr := TurnResult{Fork: t.Fork, Ask: t.Ask, Sources: []assistant.WebSource{}}
		var links *lessonctx.Links
		var linker assistant.Linker
		if t.Fork != "" {
			q.Note = assistant.NodeNote(string(t.Fork))
			x, err := r.Courses.ConceptIndexOf(ctx, c.Course, c.Version)
			if err != nil {
				return err
			}
			tr.Linking = &Linking{Index: x.Status, Candidates: []LinkEntry{}, Chosen: []LinkEntry{}}
			if x.Status == course.ConceptIndexBuilt {
				links = lessonctx.NewLinks(lib, x, c.Lesson)
				linker = links.Find
			}
		}
		msgs := pinned.Messages(q)
		if n, limit := assistant.EstimateMessages(msgs), assistant.LimitsOf(turn).Input; n > limit {
			tr.Error = "context_too_long"
			res.Turns = append(res.Turns, tr)
			fail(RuleRun, fmt.Sprintf("context_too_long: %d > %d token", n, limit))
			return nil
		}
		run, w := r.watched(turn, c.Lesson, assistant.CourseTools(lib))
		out, meta, err := assistant.Followup(ctx, run, msgs, w.sink(), linker)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tr.Reply, tr.Sources, tr.Error = out.Text, meta.Sources, errorCode(err)
		tr.Stats = w.stats(meta, turn.Rates, Stats{})
		var chosen []course.CourseLink
		if links != nil {
			for _, e := range links.Offered {
				tr.Linking.Candidates = append(tr.Linking.Candidates, linkEntry(e.Name, e.Lesson, e.Section, ""))
			}
			chosen = links.Chosen(out.Links)
			for _, l := range chosen {
				tr.Linking.Chosen = append(tr.Linking.Chosen, linkEntry(l.Concept, l.Lesson, l.Section, l.Note))
			}
		}
		res.Turns = append(res.Turns, tr)
		if tr.Error != "" {
			fail(RuleRun, why(tr.Error, err))
			return nil
		}
		for _, f := range CheckReply(tr.Reply, t, r.Mermaid) {
			fail(f.Rule, f.Detail)
		}
		offered := 0
		if links != nil {
			offered = len(links.Offered)
		}
		if bad := unoffered(w.rec.linkedEntries(), offered); len(bad) > 0 {
			fail(RuleLinkCourse, "link_course 引用了不在候选里的条目 "+strings.Join(bad, "、"))
		}
		for _, l := range chosen {
			for _, no := range t.MustNotLink {
				if course.NormalizeConcept(l.Concept) == course.NormalizeConcept(no) {
					fail(RuleMustNotLink, "关联了「"+l.Concept+"」")
				}
			}
		}
		pinned.History = append(pinned.History, assistant.Exchange{Asked: q, Reply: lessonctx.WithLinks(tr.Reply, chosen)})
	}
	return nil
}

// unoffered are the entries named that are none of the n offered (c1…cn).
func unoffered(named []string, n int) []string {
	var out []string
	for _, e := range named {
		ok := false
		for i := range n {
			if strings.TrimSpace(e) == assistant.EntryRef(i) {
				ok = true
			}
		}
		if !ok && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}
