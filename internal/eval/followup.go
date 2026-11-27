package eval

import (
	"context"
	"encoding/json"
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

// LinkEntry is a candidate section of the linking step: its label, its
// lesson and section, and, once chosen, the model's sentence on it.
type LinkEntry struct {
	Label   string `json:"label"`
	Lesson  string `json:"lesson"`
	Section string `json:"section,omitempty"`
	Note    string `json:"note,omitempty"`
	// Sources are the recall paths that found a candidate.
	Sources []course.HitSource `json:"sources,omitempty"`
}

// UnmarshalJSON reads an entry of a report written before links were to
// sections, whose label was its concept (-against).
func (e *LinkEntry) UnmarshalJSON(b []byte) error {
	type plain LinkEntry
	var v struct {
		plain
		Concept string `json:"concept"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*e = LinkEntry(v.plain)
	if e.Label == "" {
		e.Label = v.Concept
	}
	return nil
}

func linkEntry(label string, lesson course.EntryLesson, section *course.EntrySection, note string) LinkEntry {
	e := LinkEntry{Label: label, Lesson: lesson.Title, Note: note}
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
	// the case is one chain, numbering its sources as a dialogue does (ADR 0013)
	cite := assistant.NewCitations(nil)
	for i, t := range c.Turns {
		n := i + 1
		fail := func(rule, detail string) {
			res.Failures = append(res.Failures, Failure{Turn: n, Rule: rule, Detail: detail})
		}
		q := assistant.Asked{Text: t.Ask}
		tr := TurnResult{Fork: t.Fork, Ask: t.Ask, Sources: []assistant.WebSource{}}
		var links *lessonctx.Links
		var linker assistant.Linker
		var x course.ConceptIndex
		if t.Fork != "" {
			q.Note = assistant.NodeNote(string(t.Fork))
			var err error
			x, err = r.Courses.ConceptIndexOf(ctx, c.Course, c.Version)
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
		run.Cite = cite
		out, meta, err := assistant.Followup(ctx, run, msgs, w.sink(), linker)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tr.Reply, tr.Sources, tr.Error = out.Text, meta.Sources, errorCode(err)
		tr.Stats = w.stats(meta, turn.Rates, Stats{})
		var chosen []course.CourseLink
		if links != nil {
			for _, e := range links.Offered {
				le := linkEntry(e.Label, e.Lesson, e.Section, "")
				le.Sources = e.Sources
				tr.Linking.Candidates = append(tr.Linking.Candidates, le)
			}
			chosen = links.Chosen(out.Links)
			for _, l := range chosen {
				tr.Linking.Chosen = append(tr.Linking.Chosen, linkEntry(l.Label, l.Lesson, l.Section, l.Note))
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
		pinned.Citable = append(pinned.Citable, meta.Sources...)
		var citable []int
		for _, s := range pinned.Citable {
			citable = append(citable, s.N)
		}
		for _, f := range CheckCitations(tr.Reply, citable) {
			fail(f.Rule, f.Detail)
		}
		offered := 0
		if links != nil {
			offered = len(links.Offered)
		}
		if bad := unoffered(w.rec.linkedEntries(), offered); len(bad) > 0 {
			fail(RuleLinkCourse, "link_course 引用了不在候选里的小节 "+strings.Join(bad, "、"))
		}
		for _, l := range chosen {
			for _, no := range t.MustNotLink {
				if linksConcept(x, l, no) {
					fail(RuleMustNotLink, "关联了「"+no+"」所在的小节")
				}
			}
		}
		pinned.History = append(pinned.History, assistant.Exchange{Asked: q, Reply: lessonctx.WithLinks(tr.Reply, chosen)})
	}
	return nil
}

// linksConcept is whether link l is labelled name, or is to a section
// where the index x places the concept name.
func linksConcept(x course.ConceptIndex, l course.CourseLink, name string) bool {
	if course.NormalizeConcept(l.Label) == course.NormalizeConcept(name) {
		return true
	}
	return slices.ContainsFunc(x.Find(name), func(e course.ConceptEntry) bool {
		return e.Lesson.Key == l.Lesson.Key && sectionID(e.Section) == sectionID(l.Section)
	})
}

// sectionID is a section's anchor, "" for the lesson's top.
func sectionID(s *course.EntrySection) string {
	if s == nil {
		return ""
	}
	return s.ID
}

// unoffered are the sections named that are none of the n offered (s1…sn).
func unoffered(named []string, n int) []string {
	var out []string
	for _, e := range named {
		ok := false
		for i := range n {
			if strings.TrimSpace(e) == assistant.CandidateRef(i) {
				ok = true
			}
		}
		if !ok && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}
