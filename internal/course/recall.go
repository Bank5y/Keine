package course

import "context"

// MaxCandidates is how many sections an answer's linking step looks at
// (ADR 0011, ADR 0015).
const MaxCandidates = 5

// HitSource is a recall path that found a candidate section (ADR 0015).
type HitSource string

// HitConcept: the answer names a concept of the section, by name or alias.
const HitConcept HitSource = "concept"

// SectionCandidate is a section of another lesson that an answer may link
// to: where it is, what the linking step calls it, and the recall paths
// that found it.
type SectionCandidate struct {
	Lesson EntryLesson
	// Section is nil for the lesson's top.
	Section *EntrySection
	// Label is the concept the answer named there, or the section's title
	// when no concept was.
	Label   string
	Sources []HitSource
}

// Recaller finds the sections of a course that an answer relates to,
// best first (ADR 0015): at most one of each lesson, none of the lesson
// keyed selectionLesson (the selection's own), at most MaxCandidates.
type Recaller interface {
	Recall(ctx context.Context, answer, selectionLesson string) ([]SectionCandidate, error)
}

// Recall is the index's literal recall: the sections of the concepts the
// answer names (Mentioned), in the order it first names them.
func (x ConceptIndex) Recall(_ context.Context, answer, selectionLesson string) ([]SectionCandidate, error) {
	var ranked []SectionCandidate
	for _, e := range x.Mentioned(answer, selectionLesson, len(x.Entries)) {
		ranked = append(ranked, SectionCandidate{Lesson: e.Lesson, Section: e.Section, Label: e.Name, Sources: []HitSource{HitConcept}})
	}
	return firstPerLesson(ranked, selectionLesson), nil
}

// firstPerLesson keeps, of candidates ranked best first, the best of each
// lesson other than exceptLesson, at most MaxCandidates of them.
func firstPerLesson(ranked []SectionCandidate, exceptLesson string) []SectionCandidate {
	var out []SectionCandidate
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
