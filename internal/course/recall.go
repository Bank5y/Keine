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

// Trace is the index's recall of an answer, path by path, leaving out the
// lesson keyed selectionLesson.
func (x ConceptIndex) Trace(answer, selectionLesson string) RecallTrace {
	concepts := PathRanking{Source: HitConcept, Hits: []PathHit{}}
	for _, m := range x.Mentioned(answer, selectionLesson) {
		concepts.Hits = append(concepts.Hits, PathHit{Lesson: m.Entry.Lesson, Section: m.Entry.Section, Label: m.Entry.Name, Score: float64(m.At)})
	}
	paths := []PathRanking{concepts}
	return RecallTrace{Paths: paths, Candidates: fuse(paths, selectionLesson)}
}

// Recall is the fused candidates of Trace.
func (x ConceptIndex) Recall(_ context.Context, answer, selectionLesson string) ([]SectionCandidate, error) {
	return x.Trace(answer, selectionLesson).Candidates, nil
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
