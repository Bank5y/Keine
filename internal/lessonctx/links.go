package lessonctx

import (
	"context"
	"strings"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// Links links the answer of the turn a node forks with to the course
// (ADR 0011, ADR 0015) by what a recaller finds in the version the reader
// reads, and remembers the sections it offered the model.
type Links struct {
	lib    *Library
	recall course.Recaller
	except string
	// Offered are the sections Find gave the model, in the order it gave
	// them: a link's Candidate is a place in it.
	Offered []course.SectionCandidate
}

// NewLinks links answers by what r recalls of the course lib reads,
// leaving out the lesson keyed selectionLesson (the selection's own).
func NewLinks(lib *Library, r course.Recaller, selectionLesson string) *Links {
	return &Links{lib: lib, recall: r, except: selectionLesson}
}

// Find is an assistant.Linker: the sections of the course's other lessons
// the recaller finds for the answer, each with its text (its lesson's for
// the lesson's top). A lesson the reader cannot read now is not linked.
func (l *Links) Find(ctx context.Context, answer string) ([]assistant.LinkCandidate, error) {
	found, err := l.recall.Recall(ctx, answer, l.except)
	if err != nil {
		return nil, err
	}
	var out []assistant.LinkCandidate
	for _, c := range found {
		text, err := l.lib.Text(ctx, c.Lesson.Key)
		if err != nil {
			continue
		}
		lc := assistant.LinkCandidate{Label: c.Label, Lesson: c.Lesson.Title, Text: text}
		if c.Section != nil {
			lc.Section = c.Section.Title
			for _, sec := range anchor.Sections(text) {
				if sec.ID == c.Section.ID {
					lc.Text = text[sec.Start:sec.End]
					break
				}
			}
		}
		l.Offered = append(l.Offered, c)
		out = append(out, lc)
	}
	return out, nil
}

// Chosen is the course links the model picked of the sections offered.
func (l *Links) Chosen(picked []assistant.CourseLink) []course.CourseLink {
	var out []course.CourseLink
	for _, p := range picked {
		c := l.Offered[p.Candidate]
		out = append(out, course.CourseLink{Label: c.Label, Note: p.Note, Lesson: c.Lesson, Section: c.Section})
	}
	return out
}

// WithLinks is a turn's reply as the reader saw it and as later turns read
// it: the answer, then the sections it linked, if any (ADR 0011).
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
