package course

import (
	"context"
	"errors"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
)

// Suggestion is the magic wand's proposal for one annotation awaiting
// migration: a placement in the lesson's text as it is now, exactly as a
// selection of that place would give it (so accepting it is placing it
// there), where that is, and how sure the wand is, from 0 to 1.
type Suggestion struct {
	AnnotationID int64 `json:"annotationId"`
	Placement
	Range      anchor.Range `json:"range"`
	Confidence float64      `json:"confidence"`
}

// Suggestions proposes a place in the lesson's current text for each of the
// viewer's annotations on it that await migration, by deterministic fuzzy
// matching (ADR-0003). An annotation whose text is gone gets none and is
// left to the reader.
func (s *Service) Suggestions(ctx context.Context, viewer auth.User, courseID, lessonID int64) ([]Suggestion, error) {
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if errors.Is(err, ErrNotAnnotatable) {
		return []Suggestion{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT a.id, a.quote, a.prefix, a.suffix, coalesce(p.markdown, '')
		FROM annotations a LEFT JOIN previous_texts p ON p.id = a.previous_text_id
		WHERE a.user_id = $1 AND a.course_id = $2 AND a.lesson_key = $3 AND a.state = $4
		ORDER BY a.created_at, a.id`, viewer.ID, courseID, l.Key, AnnotationPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// the previous texts the annotations were last shown in, prepared once
	was := map[string]anchor.Text{}
	out := []Suggestion{}
	for rows.Next() {
		var id int64
		var q anchor.Quote
		var previous string
		if err := rows.Scan(&id, &q.Exact, &q.Prefix, &q.Suffix, &previous); err != nil {
			return nil, err
		}
		old, ok := was[previous]
		if !ok {
			old = anchor.MarkdownText(previous)
			was[previous] = old
		}
		if sg, ok := text.Suggest(q, old); ok {
			out = append(out, Suggestion{AnnotationID: id, Range: sg.Range, Confidence: sg.Confidence,
				Placement: Placement{Quote: sg.Quote.Exact, Prefix: sg.Quote.Prefix, Suffix: sg.Quote.Suffix}})
		}
	}
	return out, rows.Err()
}
