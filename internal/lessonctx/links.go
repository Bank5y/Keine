package lessonctx

import (
	"context"
	"strings"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// MaxCandidates is how many concepts an answer's linking step looks at
// (ADR 0011).
const MaxCandidates = 5

// Links links the answer of the turn a node forks with to the course
// (ADR 0011) by the concept index of the version the reader reads, and
// remembers the entries it offered the model.
type Links struct {
	lib    *Library
	index  course.ConceptIndex
	except string
	// Offered are the entries Find gave the model, in the order it gave
	// them: a link's Candidate is a place in it.
	Offered []course.ConceptEntry
}

// NewLinks links answers by the index x of the course lib reads, leaving
// out the lesson keyed exceptLesson (the selection's own).
func NewLinks(lib *Library, x course.ConceptIndex, exceptLesson string) *Links {
	return &Links{lib: lib, index: x, except: exceptLesson}
}

// Find is an assistant.Linker: the concepts of the course's other lessons
// the answer names, each with the text of its section (its lesson's when
// it has none). A lesson the reader cannot read now is not linked.
func (l *Links) Find(ctx context.Context, answer string) ([]assistant.LinkCandidate, error) {
	var out []assistant.LinkCandidate
	for _, e := range l.index.Mentioned(answer, l.except, MaxCandidates) {
		text, err := l.lib.Text(ctx, e.Lesson.Key)
		if err != nil {
			continue
		}
		lc := assistant.LinkCandidate{Concept: e.Name, Lesson: e.Lesson.Title, Text: text}
		if e.Section != nil {
			lc.Section = e.Section.Title
			for _, sec := range anchor.Sections(text) {
				if sec.ID == e.Section.ID {
					lc.Text = text[sec.Start:sec.End]
					break
				}
			}
		}
		l.Offered = append(l.Offered, e)
		out = append(out, lc)
	}
	return out, nil
}

// Chosen is the course links the model picked of the entries offered.
func (l *Links) Chosen(picked []assistant.CourseLink) []course.CourseLink {
	var out []course.CourseLink
	for _, p := range picked {
		e := l.Offered[p.Candidate]
		out = append(out, course.CourseLink{Concept: e.Name, Note: p.Note, Lesson: e.Lesson, Section: e.Section})
	}
	return out
}

// WithLinks is a turn's reply as the reader saw it and as later turns read
// it: the answer, then what it linked of the course, if anything (ADR 0011).
func WithLinks(reply string, links []course.CourseLink) string {
	if len(links) == 0 {
		return reply
	}
	var b strings.Builder
	b.WriteString(reply + "\n\n课程里的相关内容：")
	for _, l := range links {
		where := "讲「" + l.Lesson.Title + "」"
		if l.Section != nil {
			where += " › 小节「" + l.Section.Title + "」"
		}
		b.WriteString("\n- " + where + "：" + l.Note)
	}
	return b.String()
}
