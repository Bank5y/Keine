package course

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/notify"
)

// lessonFiles maps each of a course's current lessons to its text file
// ("" for a lesson without text).
func lessonFiles(ctx context.Context, tx pgx.Tx, courseID int64) (map[string]string, error) {
	rows, err := tx.Query(ctx, "SELECT key, coalesce(file, '') FROM lessons WHERE course_id = $1", courseID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	var key, file string
	_, err = pgx.ForEachRow(rows, []any{&key, &file}, func() error {
		out[key] = file
		return nil
	})
	return out, err
}

func (s *Service) readFile(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.files.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxLessonSize))
}

// notifyUpdated tells a course's subscribers, but not whoever made the
// update, that its content changed.
func notifyUpdated(ctx context.Context, tx pgx.Tx, courseID, actorID int64, p notify.UpdatedPayload) error {
	var err error
	if p.CoursePayload, err = coursePayload(ctx, tx, courseID); err != nil {
		return err
	}
	subs, err := subscribers(ctx, tx, courseID)
	if err != nil {
		return err
	}
	for _, u := range subs {
		if u == actorID {
			continue
		}
		if err := notify.Send(ctx, tx, u, notify.CourseUpdated, p); err != nil {
			return err
		}
	}
	return nil
}

// turnedDown is a pending request settled because its course left the
// library or was deleted.
type turnedDown struct {
	requester int64
	kind      string
	filesDir  string
}

// leaving is a course leaving the library (unpublished or deleted): who
// made it happen, why, and the pending requests it turned down.
type leaving struct {
	courseID int64
	actorID  int64
	reason   string
	settled  []turnedDown
}

// settleLeaving rejects a course's pending requests (of kind, or all when
// kind is "") with reason, as actorID takes the course out of the library.
func settleLeaving(ctx context.Context, tx pgx.Tx, courseID int64, kind, reason string, actorID int64) (leaving, error) {
	l := leaving{courseID: courseID, actorID: actorID, reason: reason}
	rows, err := tx.Query(ctx, `
		UPDATE review_requests SET state = $5, reason = $3, decided_at = now(), decided_by = $4
		WHERE course_id = $1 AND ($2 = '' OR kind = $2) AND state = $6
		RETURNING requester_id, kind, coalesce(files_dir, '')`, courseID, kind, reason, actorID, reviewRejected, reviewPending)
	if err != nil {
		return l, err
	}
	var r turnedDown
	_, err = pgx.ForEachRow(rows, []any{&r.requester, &r.kind, &r.filesDir}, func() error {
		l.settled = append(l.settled, r)
		return nil
	})
	return l, err
}

// notifyLeaving tells the users of a course leaving the library (kind
// course_unpublished or course_deleted) and then the requesters whose
// requests it turned down, why. Each user gets payload(course, user), or
// the course payload itself when payload is nil. Nobody is told twice, and
// the user who made it happen is not told at all.
func notifyLeaving(ctx context.Context, tx pgx.Tx, l leaving, kind notify.Kind, users []int64,
	payload func(notify.CoursePayload, int64) (any, error)) error {
	cp, err := coursePayload(ctx, tx, l.courseID)
	if err != nil {
		return err
	}
	told := map[int64]bool{l.actorID: true}
	for _, u := range users {
		if told[u] {
			continue
		}
		told[u] = true
		var p any = cp
		if payload != nil {
			if p, err = payload(cp, u); err != nil {
				return err
			}
		}
		if err := notify.Send(ctx, tx, u, kind, p); err != nil {
			return err
		}
	}
	for _, r := range l.settled {
		if told[r.requester] {
			continue
		}
		told[r.requester] = true
		k := notify.UpdateRejected
		if r.kind == reviewPublish {
			k = notify.PublishRejected
		}
		if err := notify.Send(ctx, tx, r.requester, k, notify.RejectedPayload{CoursePayload: cp, Reason: l.reason}); err != nil {
			return err
		}
	}
	return nil
}

func coursePayload(ctx context.Context, tx pgx.Tx, courseID int64) (notify.CoursePayload, error) {
	p := notify.CoursePayload{CourseID: courseID}
	err := tx.QueryRow(ctx, "SELECT title FROM courses WHERE id = $1", courseID).Scan(&p.CourseTitle)
	return p, err
}

// subscribers lists the users subscribed to a course.
func subscribers(ctx context.Context, tx pgx.Tx, courseID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, "SELECT user_id FROM course_readers WHERE course_id = $1 AND subscribed_at IS NOT NULL ORDER BY user_id", courseID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
