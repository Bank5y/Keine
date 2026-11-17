package course

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/notify"
)

// A draft nobody works on expires (ADR-0006): DraftLifetime after its last
// change it is deleted with its temporary area, and DraftReminder before
// that its owner is told once. Any change to the draft starts it over.
const (
	DraftLifetime = 30 * 24 * time.Hour
	DraftReminder = 5 * 24 * time.Hour
)

// SweepDrafts reminds the owners of drafts about to expire and deletes the
// expired ones, as of the service's clock. It is meant to run periodically.
func (s *Service) SweepDrafts(ctx context.Context) error {
	now := s.now()
	if err := s.deleteExpiredDrafts(ctx, now); err != nil {
		return err
	}
	if err := s.remindExpiringDrafts(ctx, now); err != nil {
		return err
	}
	return s.dropOrphanDraftFiles(ctx)
}

// dropOrphanDraftFiles removes the temporary areas of deleted drafts that
// are still there: a draft's deletion, however it happens (a deleted user's
// drafts go with them), records its area in draft_area_drops, and a row
// stays until removing the area worked.
func (s *Service) dropOrphanDraftFiles(ctx context.Context) error {
	rows, err := s.db.Query(ctx, "SELECT ARRAY[draft_id, course_id] FROM draft_area_drops ORDER BY draft_id")
	if err != nil {
		return err
	}
	left, err := pgx.CollectRows(rows, pgx.RowTo[[2]int64])
	if err != nil {
		return err
	}
	for _, d := range left {
		s.dropDraftFiles(ctx, d[1], d[0], "")
	}
	return nil
}

// deleteExpiredDrafts deletes the drafts untouched for DraftLifetime, then
// their temporary areas.
func (s *Service) deleteExpiredDrafts(ctx context.Context, now time.Time) error {
	rows, err := s.db.Query(ctx, "DELETE FROM course_drafts WHERE updated_at <= $1 RETURNING ARRAY[course_id, id]", now.Add(-DraftLifetime))
	if err != nil {
		return err
	}
	expired, err := pgx.CollectRows(rows, pgx.RowTo[[2]int64])
	if err != nil {
		return err
	}
	for _, d := range expired {
		s.dropDraftFiles(ctx, d[0], d[1], "")
	}
	return nil
}

// remindExpiringDrafts tells each owner of a draft within DraftReminder of
// expiring, once per idle spell.
func (s *Service) remindExpiringDrafts(ctx context.Context, now time.Time) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			UPDATE course_drafts d SET reminded_at = $1
			FROM courses c
			WHERE c.id = d.course_id AND d.updated_at <= $2
				AND (d.reminded_at IS NULL OR d.reminded_at <= d.updated_at)
			RETURNING d.user_id, c.id, c.title, d.updated_at`,
			now, now.Add(DraftReminder-DraftLifetime))
		if err != nil {
			return err
		}
		type due struct {
			user    int64
			p       notify.ExpiringPayload
			updated time.Time
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
			var d due
			err := r.Scan(&d.user, &d.p.CourseID, &d.p.CourseTitle, &d.updated)
			return d, err
		})
		if err != nil {
			return err
		}
		for _, d := range list {
			d.p.ExpiresAt = d.updated.Add(DraftLifetime)
			if err := notify.Send(ctx, tx, d.user, notify.DraftExpiring, d.p); err != nil {
				return err
			}
		}
		return nil
	})
}
