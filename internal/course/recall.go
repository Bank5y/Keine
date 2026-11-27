package course

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
)

// MaxCandidates is how many sections an answer's linking step looks at
// (ADR 0011, ADR 0015).
const MaxCandidates = 5

// rrfK is the k of reciprocal rank fusion: a hit at rank r of a path adds
// 1/(rrfK+r) to its section's score (ADR 0015).
const rrfK = 60

// HitSource is a recall path that found a candidate section (ADR 0015).
type HitSource string

// HitConcept: the answer names a concept of the section, by name or alias.
const HitConcept HitSource = "concept"

// SectionCandidate is a section of another lesson that an answer may link
// to: where it is, what the linking step calls it, the recall paths that
// found it, and its fused score.
type SectionCandidate struct {
	Lesson EntryLesson `json:"lesson"`
	// Section is nil for the lesson's top.
	Section *EntrySection `json:"section"`
	// Label is the concept the answer named there, or the section's title
	// when no concept was.
	Label   string      `json:"label"`
	Sources []HitSource `json:"sources"`
	Score   float64     `json:"score"`
}

// PathHit is a section a recall path found, with the path's own score:
// for the concept path, where the answer first names the concept (lower
// is better); for the others, higher is better.
type PathHit struct {
	Lesson  EntryLesson   `json:"lesson"`
	Section *EntrySection `json:"section"`
	// Label is the concept named, or "" when the path finds no concept.
	Label string  `json:"label"`
	Score float64 `json:"score"`
	// Weight multiplies the hit's share of the fused score: a concept's
	// revised weight (#226); 0 is 1.
	Weight float64 `json:"weight,omitempty"`
}

// PathRanking is what one recall path found, best first.
type PathRanking struct {
	Source HitSource `json:"source"`
	Hits   []PathHit `json:"hits"`
}

// RecallTrace is a recall step laid open (#222): each path's ranking, and
// the candidates they fused into, which are what the linking step is
// offered.
type RecallTrace struct {
	Paths      []PathRanking      `json:"paths"`
	Candidates []SectionCandidate `json:"candidates"`
	// Revisions are the course's revisions that changed this recall.
	Revisions []RevisionEffect `json:"revisions"`
}

// RevisionEffect is how a revision changed a recall (#226).
type RevisionEffect struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Effect string `json:"effect"`
}

// Recaller finds the sections of a course that an answer relates to,
// best first (ADR 0015): at most one of each lesson, none of the lesson
// keyed selectionLesson (the selection's own), at most MaxCandidates.
type Recaller interface {
	Recall(ctx context.Context, answer, selectionLesson string) ([]SectionCandidate, error)
}

// maxPathHits is how many hits of each path a recall keeps for fusing
// and for the recall test to show.
const maxPathHits = 50

// CourseRecall recalls from a course version's concept index and its
// passages (ADR 0015).
type CourseRecall struct {
	Concepts  ConceptIndex
	Passages  Passages
	Revisions []Revision
	// Embed embeds an answer for the vector path (#227); nil while
	// embedding is off, and then there is no vector path.
	Embed func(ctx context.Context, text string) ([]float32, error)
}

// RecallOf is what recalls from a version of a course: its concept index
// (no entries until built), its passages and the course's revisions. With
// generation above 0, the passages have their vectors of it, for embed
// to recall by (#227).
func (s *Service) RecallOf(ctx context.Context, courseID int64, filesDir string, generation int64, embed func(context.Context, string) ([]float32, error)) (CourseRecall, error) {
	x, err := s.ConceptIndexOf(ctx, courseID, filesDir)
	if err != nil {
		return CourseRecall{}, err
	}
	p, err := s.PassagesOf(ctx, courseID, filesDir)
	if err != nil {
		return CourseRecall{}, err
	}
	if embed != nil && generation > 0 {
		if err := s.withVectors(ctx, &p, courseID, filesDir, generation); err != nil {
			return CourseRecall{}, err
		}
	}
	revs, err := s.revisions(ctx, courseID)
	return CourseRecall{Concepts: x, Passages: p, Revisions: revs, Embed: embed}, err
}

// Trace is the recall of an answer, path by path, leaving out the lesson
// keyed selectionLesson, with the course's revisions laid over it (#226).
func (r CourseRecall) Trace(ctx context.Context, answer, selectionLesson string) RecallTrace {
	var effects []RevisionEffect
	effect := func(rev *Revision, label, what string) {
		if !slices.ContainsFunc(effects, func(e RevisionEffect) bool { return e.ID == rev.ID }) {
			effects = append(effects, RevisionEffect{ID: rev.ID, Kind: rev.Kind, Label: label, Effect: what})
		}
	}
	recallable, disabled := r.Concepts.recallable(selectionLesson, r.Revisions)
	concepts := PathRanking{Source: HitConcept, Hits: []PathHit{}}
	for _, m := range recallable.Mentioned(answer, selectionLesson) {
		concepts.Hits = append(concepts.Hits, PathHit{Lesson: m.Entry.Lesson, Section: m.Entry.Section, Label: m.Entry.Name, Score: float64(m.At), Weight: m.Entry.weight})
		if rev := m.Entry.revision; rev != nil {
			what := "修订过的概念"
			switch {
			case rev.Kind == RevisionNew:
				what = "手动新增的概念"
			case m.Entry.weight != 0 && m.Entry.weight != 1:
				what = "权重 ×" + strconv.FormatFloat(m.Entry.weight, 'f', -1, 64)
			}
			effect(rev, m.Entry.Name, what)
		}
	}
	for _, m := range disabled.Mentioned(answer, selectionLesson) {
		effect(m.Entry.revision, m.Entry.Name, "已禁用, 不参与召回")
	}
	passages := PathRanking{Source: HitPassage, Hits: r.Passages.rank(answer, selectionLesson)}
	paths := []PathRanking{concepts, passages}
	if r.Embed != nil {
		// an embedding that fails leaves the vector path empty, not the recall
		vectors := PathRanking{Source: HitVector, Hits: []PathHit{}}
		if q, err := r.Embed(ctx, answer); err == nil {
			vectors.Hits = r.Passages.rankVector(q, selectionLesson)
		} else if ctx.Err() == nil {
			slog.Warn("answer not embedded", "err", err)
		}
		paths = append(paths, vectors)
	}
	for i := range paths {
		kept := paths[i].Hits[:0]
		for _, h := range paths[i].Hits {
			if rev := excluding(r.Revisions, h); rev != nil {
				label := h.Lesson.Title
				if rev.Section != nil {
					label += " › " + rev.Section.Title
				}
				effect(rev, label, "已排除")
				continue
			}
			kept = append(kept, h)
		}
		paths[i].Hits = kept[:min(len(kept), maxPathHits)]
	}
	if effects == nil {
		effects = []RevisionEffect{}
	}
	return RecallTrace{Paths: paths, Candidates: fuse(paths, selectionLesson), Revisions: effects}
}

// excluding is the exclusion that leaves hit out: of its lesson, or of its
// section.
func excluding(revs []Revision, h PathHit) *Revision {
	for i, r := range revs {
		if r.Kind != RevisionExclude || r.Lesson == nil || r.Lesson.Key != h.Lesson.Key {
			continue
		}
		if r.Section == nil || h.Section != nil && h.Section.ID == r.Section.ID {
			return &revs[i]
		}
	}
	return nil
}

// recallable is what the concept path matches answers against, and the
// concepts revisions disabled: the index's concepts once merged, each
// placed at its primary section, or at its best other one when that is in
// the lesson keyed except, or the entries as extracted before any were
// merged; with the revisions laid over them (a name, aliases, a weight;
// generic ones and disabled ones left out) and the concepts added.
func (x ConceptIndex) recallable(except string, revs []Revision) (ConceptIndex, ConceptIndex) {
	byKey := map[string]*Revision{}
	for i, r := range revs {
		if r.Kind == RevisionConcept {
			byKey[r.key] = &revs[i]
		}
	}
	out, off := ConceptIndex{Status: x.Status}, ConceptIndex{Status: x.Status}
	add := func(e ConceptEntry, generic bool) {
		rev := byKey[NormalizeConcept(e.Name)]
		if rev != nil {
			e.revision = rev
			if rev.Name != nil {
				e.Name = *rev.Name
			}
			if rev.Aliases != nil {
				e.Aliases = rev.Aliases
			}
			if rev.Summary != nil {
				e.Summary = *rev.Summary
			}
			if rev.Generic != nil {
				generic = *rev.Generic
			}
			e.weight = rev.Weight
			if rev.Disabled {
				off.Entries = append(off.Entries, e)
				return
			}
		}
		if !generic {
			out.Entries = append(out.Entries, e)
		}
	}
	if len(x.Concepts) == 0 {
		for _, e := range x.Entries {
			add(e, false)
		}
	}
	for _, c := range x.Concepts {
		at := c.Primary
		if at.Lesson.Key == except {
			at = primaryOf(c.Occurrences, c.Name, except)
		}
		add(ConceptEntry{Name: c.Name, Aliases: c.Aliases, Lesson: at.Lesson, Section: at.Section, Summary: c.Summary}, c.Generic)
	}
	for i, r := range revs {
		if r.Kind == RevisionNew && r.Lesson != nil && r.Name != nil {
			out.Entries = append(out.Entries, ConceptEntry{Name: *r.Name, Aliases: r.Aliases, Lesson: *r.Lesson, Section: r.Section, revision: &revs[i], weight: r.Weight})
		}
	}
	return out, off
}

// Recall is the fused candidates of Trace.
func (r CourseRecall) Recall(ctx context.Context, answer, selectionLesson string) ([]SectionCandidate, error) {
	return r.Trace(ctx, answer, selectionLesson).Candidates, nil
}

// fuse merges the paths' rankings by section with reciprocal rank fusion
// (a section counts once per path, at its best rank), then keeps the best
// section of each lesson other than exceptLesson, at most MaxCandidates.
// A candidate's label is the concept of its best concept hit, or its
// section's title (its lesson's, at the top) when none named one.
func fuse(paths []PathRanking, exceptLesson string) []SectionCandidate {
	type key struct {
		lesson  string
		section string
	}
	var order []key
	found := map[key]*SectionCandidate{}
	for _, p := range paths {
		seen := map[key]bool{}
		for rank, h := range p.Hits {
			k := key{h.Lesson.Key, ""}
			if h.Section != nil {
				k.section = h.Section.ID
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			c, ok := found[k]
			if !ok {
				c = &SectionCandidate{Lesson: h.Lesson, Section: h.Section}
				found[k] = c
				order = append(order, k)
			}
			w := h.Weight
			if w == 0 {
				w = 1
			}
			c.Score += w / float64(rrfK+rank+1)
			c.Sources = append(c.Sources, p.Source)
			if c.Label == "" && h.Label != "" {
				c.Label = h.Label
			}
		}
	}
	ranked := make([]SectionCandidate, 0, len(order))
	for _, k := range order {
		c := *found[k]
		if c.Label == "" {
			c.Label = c.Lesson.Title
			if c.Section != nil {
				c.Label = c.Section.Title
			}
		}
		ranked = append(ranked, c)
	}
	// stable: of equal scores, the one found first stays first
	slices.SortStableFunc(ranked, func(a, b SectionCandidate) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	return firstPerLesson(ranked, exceptLesson)
}

// firstPerLesson keeps, of candidates ranked best first, the best of each
// lesson other than exceptLesson, at most MaxCandidates of them.
func firstPerLesson(ranked []SectionCandidate, exceptLesson string) []SectionCandidate {
	out := []SectionCandidate{}
	seen := map[string]bool{exceptLesson: true}
	for _, c := range ranked {
		if len(out) == MaxCandidates {
			break
		}
		if !seen[c.Lesson.Key] {
			seen[c.Lesson.Key] = true
			out = append(out, c)
		}
	}
	return out
}
