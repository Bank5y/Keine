package course

import (
	"context"
	"strings"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
)

// Passage is a selection in a lesson the viewer may annotate, with what
// the exploration assistant reads around it.
type Passage struct {
	Course  Detail
	Lesson  LessonText
	Chapter string
	// Headings are the headings above the selection, outermost first; one
	// that only repeats the lesson title is left out.
	Headings []string
	// Start and End are where the selection sits in Lesson.Markdown, in
	// bytes; Marked is false when it could not be told apart there.
	Start, End int
	Marked     bool
}

// Passage finds a selection in a lesson the viewer may annotate, the way an
// annotation would be placed: ErrNotAnnotatable for a lesson without
// column text, ErrQuoteNotFound when the text does not hold the quote.
func (s *Service) Passage(ctx context.Context, viewer auth.User, courseID, lessonID int64, q anchor.Quote) (Passage, error) {
	if !validQuote(q.Exact, q.Prefix, q.Suffix) {
		return Passage{}, ErrBadInput
	}
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if err != nil {
		return Passage{}, err
	}
	r, ok := text.Locate(q)
	if !ok {
		return Passage{}, ErrQuoteNotFound
	}
	d, err := s.Get(ctx, viewer, courseID)
	if err != nil {
		return Passage{}, err
	}
	p := Passage{Course: d, Lesson: l}
	for _, ch := range d.Chapters {
		if ch.ID == l.ChapterID {
			p.Chapter = ch.Title
		}
	}
	p.Start, p.End, p.Marked = anchor.SourceSpan(l.Markdown, r)
	if p.Marked {
		for _, h := range anchor.HeadingsAt(l.Markdown, p.Start) {
			if strings.TrimSpace(h) != strings.TrimSpace(l.Title) {
				p.Headings = append(p.Headings, h)
			}
		}
	}
	return p, nil
}
