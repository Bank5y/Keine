package course

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
)

// Style is how an annotation marks its text.
type Style string

// Annotation styles and the colours of a highlight.
const (
	StyleUnderline Style = "underline"
	StyleHighlight Style = "highlight"
)

var highlightColors = map[string]bool{"yellow": true, "green": true, "blue": true, "pink": true}

var (
	ErrAnnotationNotFound = errors.New("annotation not found")
	// ErrNotAnnotatable: only the text of a column lesson takes annotations.
	ErrNotAnnotatable = errors.New("lesson takes no annotations")
	// ErrQuoteNotFound: the quoted text is not in the lesson.
	ErrQuoteNotFound = errors.New("quote not in the lesson text")
)

// limits on what an annotation keeps, in characters
const (
	maxQuote   = 20000
	maxContext = 200
	maxNote    = 20000
)

// Annotation is one of the viewer's annotations on a lesson.
type Annotation struct {
	ID     int64           `json:"id"`
	Style  Style           `json:"style"`
	Color  *string         `json:"color"`
	Quote  string          `json:"quote"`
	Prefix string          `json:"prefix"`
	Suffix string          `json:"suffix"`
	Note   *string         `json:"note"`
	State  AnnotationState `json:"state"`
	// PreviousTextID is, while pending, the previous text the annotation was
	// last shown in (see PreviousTexts); nil when it has none.
	PreviousTextID *int64 `json:"previousTextId"`
	// Range is where the quote sits in the lesson text as it is now; nil
	// unless the annotation is attached and the text holds it.
	Range     *anchor.Range `json:"range"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// NewAnnotation is what a reader sends to annotate a selection.
type NewAnnotation struct {
	Style  Style  `json:"style"`
	Color  string `json:"color"`
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	// Note is optional; a blank one is no note.
	Note string `json:"note"`
}

// AnnotationStyle restyles an annotation.
type AnnotationStyle struct {
	Style Style  `json:"style"`
	Color string `json:"color"`
}

// color validates a style and returns the colour to store with it.
func (a AnnotationStyle) color() (*string, error) {
	switch {
	case a.Style == StyleUnderline && a.Color == "":
		return nil, nil
	case a.Style == StyleHighlight && highlightColors[a.Color]:
		return &a.Color, nil
	}
	return nil, ErrBadInput
}

// annotatable loads the text of a lesson the viewer may annotate.
func (s *Service) annotatable(ctx context.Context, viewer auth.User, courseID, lessonID int64) (LessonText, anchor.Text, error) {
	// a subscriber keeps annotating the lessons he retains of a frozen course
	sm, ret, err := s.open(ctx, viewer, courseID, "")
	if err != nil {
		return LessonText{}, anchor.Text{}, err
	}
	l, err := s.lesson(ctx, viewer, sm, ret, lessonID)
	if err != nil {
		return LessonText{}, anchor.Text{}, err
	}
	if sm.Type != TypeColumn || l.Markdown == "" {
		return LessonText{}, anchor.Text{}, ErrNotAnnotatable
	}
	return l, anchor.MarkdownText(l.Markdown), nil
}

const annotationColumns = "id, style, color, quote, prefix, suffix, note, state, previous_text_id, created_at, updated_at"

// scanAnnotation scans annotationColumns into a, after any leading columns
// into lead.
func scanAnnotation(row pgx.Row, a *Annotation, lead ...any) error {
	return row.Scan(append(lead, &a.ID, &a.Style, &a.Color, &a.Quote, &a.Prefix, &a.Suffix, &a.Note, &a.State,
		&a.PreviousTextID, &a.CreatedAt, &a.UpdatedAt)...)
}

// validQuote tells whether a quote and the text around it, as a selection
// gives them, may be stored: a quote that is not blank, within the limits.
func validQuote(quote, prefix, suffix string) bool {
	return anchor.NewText(quote).String() != "" && utf8.RuneCountInString(quote) <= maxQuote &&
		utf8.RuneCountInString(prefix) <= maxContext && utf8.RuneCountInString(suffix) <= maxContext
}

// locate finds an attached annotation in the lesson text.
func locate(text anchor.Text, a *Annotation) {
	if a.State != AnnotationAttached {
		return
	}
	if r, ok := text.Locate(anchor.Quote{Exact: a.Quote, Prefix: a.Prefix, Suffix: a.Suffix}); ok {
		a.Range = &r
	}
}

// Annotations lists the viewer's annotations on a lesson, in the order they
// were made, each located in the lesson text.
func (s *Service) Annotations(ctx context.Context, viewer auth.User, courseID, lessonID int64) ([]Annotation, error) {
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if errors.Is(err, ErrNotAnnotatable) {
		return []Annotation{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, "SELECT "+annotationColumns+` FROM annotations
		WHERE user_id = $1 AND course_id = $2 AND lesson_key = $3 ORDER BY created_at, id`, viewer.ID, courseID, l.Key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Annotation{}
	for rows.Next() {
		var a Annotation
		if err := scanAnnotation(rows, &a); err != nil {
			return nil, err
		}
		locate(text, &a)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Annotate saves an annotation on a lesson; the quote must be in its text.
func (s *Service) Annotate(ctx context.Context, viewer auth.User, courseID, lessonID int64, in NewAnnotation) (Annotation, error) {
	color, err := AnnotationStyle{Style: in.Style, Color: in.Color}.color()
	if err != nil {
		return Annotation{}, err
	}
	if !validQuote(in.Quote, in.Prefix, in.Suffix) {
		return Annotation{}, ErrBadInput
	}
	var note *string
	if strings.TrimSpace(in.Note) != "" {
		if utf8.RuneCountInString(in.Note) > maxNote {
			return Annotation{}, ErrBadInput
		}
		note = &in.Note
	}
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if err != nil {
		return Annotation{}, err
	}
	a := Annotation{Quote: in.Quote, Prefix: in.Prefix, Suffix: in.Suffix, State: AnnotationAttached}
	if locate(text, &a); a.Range == nil {
		return Annotation{}, ErrQuoteNotFound
	}
	err = scanAnnotation(s.db.QueryRow(ctx, `
		INSERT INTO annotations (user_id, course_id, lesson_key, style, color, quote, prefix, suffix, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+annotationColumns,
		viewer.ID, courseID, l.Key, in.Style, color, in.Quote, in.Prefix, in.Suffix, note), &a)
	return a, err
}

// Restyle changes the style of one of the viewer's annotations.
func (s *Service) Restyle(ctx context.Context, viewer auth.User, id int64, in AnnotationStyle) error {
	color, err := in.color()
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, "UPDATE annotations SET style = $3, color = $4, updated_at = now() WHERE id = $1 AND user_id = $2",
		id, viewer.ID, in.Style, color)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrAnnotationNotFound
	}
	return err
}

// WriteNote writes, or rewrites, the note on one of the viewer's
// annotations; a note is never blank.
func (s *Service) WriteNote(ctx context.Context, viewer auth.User, id int64, note string) error {
	if strings.TrimSpace(note) == "" || utf8.RuneCountInString(note) > maxNote {
		return ErrBadInput
	}
	return s.setNote(ctx, viewer, id, &note)
}

// DeleteNote takes the note off one of the viewer's annotations; the
// annotation stays.
func (s *Service) DeleteNote(ctx context.Context, viewer auth.User, id int64) error {
	return s.setNote(ctx, viewer, id, nil)
}

func (s *Service) setNote(ctx context.Context, viewer auth.User, id int64, note *string) error {
	tag, err := s.db.Exec(ctx, "UPDATE annotations SET note = $3, updated_at = now() WHERE id = $1 AND user_id = $2",
		id, viewer.ID, note)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrAnnotationNotFound
	}
	return err
}

// DeleteAnnotation removes one of the viewer's annotations.
func (s *Service) DeleteAnnotation(ctx context.Context, viewer auth.User, id int64) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM annotations WHERE id = $1 AND user_id = $2", id, viewer.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrAnnotationNotFound
	}
	return err
}
