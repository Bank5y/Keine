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

// Subscribe puts a library course in the viewer's courses; a course
// unsubscribed before comes back with its learning records. Private courses
// are always in their uploader's courses and cannot be subscribed to.
func (s *Service) Subscribe(ctx context.Context, viewer auth.User, id int64) error {
	sm, err := s.loadVisible(ctx, viewer, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Public {
		return ErrInvalidState
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO course_readers (user_id, course_id, subscribed_at) VALUES ($1, $2, now())
		ON CONFLICT (user_id, course_id) DO UPDATE SET subscribed_at = coalesce(course_readers.subscribed_at, now())`, viewer.ID, id)
	return err
}

// Unsubscribe takes a library course out of the viewer's courses. Learning
// records stay, so subscribing again picks up where the viewer left off. A
// private course cannot be unsubscribed, only deleted. Unsubscribing from a
// course that left the library gives up the retained lessons: the viewer
// can no longer see it, nor subscribe again.
func (s *Service) Unsubscribe(ctx context.Context, viewer auth.User, id int64) error {
	sm, ret, err := s.open(ctx, viewer, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Public && ret == nil {
		return ErrInvalidState
	}
	var frozen string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE course_readers SET subscribed_at = NULL WHERE user_id = $1 AND course_id = $2", viewer.ID, id); err != nil {
			return err
		}
		if ret != nil {
			frozen, err = releaseRetainer(ctx, tx, id, viewer.ID)
		}
		return err
	})
	if err == nil {
		s.dropUnused(ctx, id, frozen)
	}
	return err
}
