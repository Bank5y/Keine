package course

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// LibraryFilter narrows the library; empty fields match everything.
type LibraryFilter struct {
	Type       string
	Completion string
	// Q matches the course title or the author's name, ignoring case.
	Q string
}

// Library lists the public courses, newest first.
func (s *Service) Library(ctx context.Context, viewer auth.User, f LibraryFilter) ([]Summary, error) {
	q := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(f.Q))
	rows, err := s.db.Query(ctx, "SELECT "+summaryColumns("$1")+` FROM courses c
		WHERE c.visibility = 'public'
		  AND ($2 = '' OR c.type = $2)
		  AND ($3 = '' OR c.completion = $3)
		  AND ($4 = '' OR c.title ILIKE '%' || $4 || '%' OR c.author_name ILIKE '%' || $4 || '%')
		ORDER BY c.created_at DESC, c.id DESC`, viewer.ID, f.Type, f.Completion, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) { return scanSummary(r, viewer) })
}

// Enroll starts the viewer learning a course they can see; starting again
// changes nothing.
func (s *Service) Enroll(ctx context.Context, viewer auth.User, id int64) error {
	if _, err := s.loadVisible(ctx, viewer, id, ""); err != nil {
		return err
	}
	return enroll(ctx, s, viewer, id)
}

func enroll(ctx context.Context, s *Service, viewer auth.User, courseID int64) error {
	_, err := s.db.Exec(ctx, "INSERT INTO enrollments (user_id, course_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", viewer.ID, courseID)
	return err
}
