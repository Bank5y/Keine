package course

import (
	"context"
	"slices"
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
	Concepts ConceptIndex
	Passages Passages
}

// RecallOf is what recalls from a version of a course: its concept index
// (no entries until built) and its passages.
func (s *Service) RecallOf(ctx context.Context, courseID int64, filesDir string) (CourseRecall, error) {
	x, err := s.ConceptIndexOf(ctx, courseID, filesDir)
	if err != nil {
		return CourseRecall{}, err
	}
	p, err := s.PassagesOf(ctx, courseID, filesDir)
	return CourseRecall{Concepts: x, Passages: p}, err
}

// Trace is the recall of an answer, path by path, leaving out the lesson
// keyed selectionLesson.
func (r CourseRecall) Trace(answer, selectionLesson string) RecallTrace {
	concepts := PathRanking{Source: HitConcept, Hits: []PathHit{}}
	for _, m := range r.Concepts.recallable(selectionLesson).Mentioned(answer, selectionLesson) {
		concepts.Hits = append(concepts.Hits, PathHit{Lesson: m.Entry.Lesson, Section: m.Entry.Section, Label: m.Entry.Name, Score: float64(m.At)})
	}
	passages := PathRanking{Source: HitPassage, Hits: r.Passages.rank(answer, selectionLesson)}
	paths := []PathRanking{concepts, passages}
	for i := range paths {
		paths[i].Hits = paths[i].Hits[:min(len(paths[i].Hits), maxPathHits)]
	}
	return RecallTrace{Paths: paths, Candidates: fuse(paths, selectionLesson)}
}

// recallable is what the concept path matches answers against: the
// index's concepts once merged, generic ones left out, each placed at its
// primary section, or at its best other one when that is in the lesson
// keyed except; the entries as extracted before any were merged.
func (x ConceptIndex) recallable(except string) ConceptIndex {
	if len(x.Concepts) == 0 {
		return x
	}
	out := ConceptIndex{Status: x.Status}
	for _, c := range x.Concepts {
		if c.Generic {
			continue
		}
		at := c.Primary
		if at.Lesson.Key == except {
			at = primaryOf(c.Occurrences, c.Name, except)
		}
		out.Entries = append(out.Entries, ConceptEntry{Name: c.Name, Aliases: c.Aliases, Lesson: at.Lesson, Section: at.Section, Summary: c.Summary})
	}
	return out
}

// Recall is the fused candidates of Trace.
func (r CourseRecall) Recall(_ context.Context, answer, selectionLesson string) ([]SectionCandidate, error) {
	return r.Trace(answer, selectionLesson).Candidates, nil
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
			c.Score += 1 / float64(rrfK+rank+1)
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
