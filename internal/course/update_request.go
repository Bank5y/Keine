package course

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
)

// ErrUpdateNotAllowed means only the uploader may update a finished course.
var ErrUpdateNotAllowed = errors.New("only the uploader may update a finished course")

// SubmitUpdate asks for a public course's content to be replaced by a new
// course package. The uploader may always ask; anyone may while the course
// is ongoing or incomplete. The package is checked now and stored in its own
// folder; the course does not change until an admin approves.
func (s *Service) SubmitUpdate(ctx context.Context, viewer auth.User, id int64, zr *zip.Reader) (int64, error) {
	sm, err := s.loadVisible(ctx, viewer, id, "")
	if err != nil {
		return 0, err
	}
	if sm.Visibility != Public {
		return 0, ErrInvalidState // a private course is updated directly
	}
	if !sm.IsUploader && sm.Completion == Finished {
		return 0, ErrUpdateNotAllowed
	}
	pp, err := parsePackage(zr)
	if err != nil {
		return 0, err
	}
	dir := fmt.Sprintf("r%d", time.Now().UnixNano())
	if err := s.storeFiles(ctx, pp, filesPrefix(id, dir)); err != nil {
		s.dropFiles(ctx, id, dir)
		return 0, err
	}
	var reqID int64
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO review_requests (kind, course_id, requester_id, files_dir, base_dir, manifest)
			VALUES ('update', $1, $2, $3, $4, $5) RETURNING id`, id, viewer.ID, dir, sm.FilesDir, pp.raw).Scan(&reqID); err != nil {
			return err
		}
		return tellAdmins(ctx, tx, notify.UpdateRequested, id, reqID, viewer)
	})
	if err != nil {
		s.dropFiles(ctx, id, dir)
	}
	return reqID, err
}

// UpdateRequest is one of the viewer's update requests for a course.
type UpdateRequest struct {
	ID        int64      `json:"id"`
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt"`
}

// MyUpdateRequests lists the viewer's update requests for a course, newest
// first.
func (s *Service) MyUpdateRequests(ctx context.Context, viewer auth.User, id int64) ([]UpdateRequest, error) {
	if _, err := s.loadVisible(ctx, viewer, id, ""); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, state, reason, created_at, decided_at FROM review_requests
		WHERE kind = 'update' AND course_id = $1 AND requester_id = $2
		ORDER BY created_at DESC, id DESC`, id, viewer.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[UpdateRequest])
}

// WithdrawUpdate takes back the viewer's pending update request.
func (s *Service) WithdrawUpdate(ctx context.Context, viewer auth.User, courseID, reqID int64) error {
	var dir string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE review_requests SET state = 'withdrawn', decided_at = now()
			WHERE id = $1 AND course_id = $2 AND requester_id = $3 AND kind = 'update' AND state = 'pending'
			RETURNING coalesce(files_dir, '')`, reqID, courseID, viewer.ID).Scan(&dir)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReviewNotFound
		}
		if err != nil {
			return err
		}
		return closeRequestNotices(ctx, tx, courseID)
	})
	if err == nil && dir != "" {
		s.dropFiles(ctx, courseID, dir)
	}
	return err
}

// Changes compares a manifest's lessons with a course's current ones.
type Changes struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Kept    int      `json:"kept"`
}

func (s *Service) changes(ctx context.Context, courseID int64, m Manifest) (*Changes, error) {
	rows, err := s.db.Query(ctx, `
		SELECT l.key, l.title, `+upcoming+` FROM lessons l JOIN chapters ch ON ch.id = l.chapter_id
		WHERE l.course_id = $1 ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return nil, err
	}
	type lesson struct {
		Key, Title string
		Upcoming   bool
	}
	current, err := pgx.CollectRows(rows, pgx.RowToStructByPos[lesson])
	if err != nil {
		return nil, err
	}
	// Only lessons with content count: an upcoming lesson that gets content
	// is added, one that loses it is removed.
	incoming := map[string]bool{}
	c := &Changes{Added: []string{}, Removed: []string{}}
	have := map[string]bool{}
	for _, l := range current {
		have[l.Key] = !l.Upcoming
	}
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			if l.Upcoming() {
				continue
			}
			incoming[l.Key] = true
			if have[l.Key] {
				c.Kept++
			} else {
				c.Added = append(c.Added, l.Title)
			}
		}
	}
	for _, l := range current {
		if !l.Upcoming && !incoming[l.Key] {
			c.Removed = append(c.Removed, l.Title)
		}
	}
	return c, nil
}
