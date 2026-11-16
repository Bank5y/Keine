package course

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
)

var (
	ErrInvalidState   = errors.New("not allowed in the course's current state")
	ErrReasonRequired = errors.New("a rejection needs a reason")
	ErrReviewNotFound = errors.New("review request not found")
	ErrBadInput       = errors.New("invalid input")
	// a user who is not the uploader cannot even see a private course
	errNotUploader = ErrNotFound
)

// Review request kinds and states.
const (
	reviewPublish = "publish"
	reviewUpdate  = "update"

	reviewApproved  = "approved"
	reviewRejected  = "rejected"
	reviewWithdrawn = "withdrawn"
)

// RequestPublish asks for the uploader's private course to join the library.
func (s *Service) RequestPublish(ctx context.Context, viewer auth.User, id int64) error {
	sm, err := s.loadVisible(ctx, viewer, id, "")
	if err != nil {
		return err
	}
	if !sm.IsUploader {
		return errNotUploader
	}
	if sm.Visibility != Private || sm.PublishState == Pending {
		return ErrInvalidState
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO review_requests (kind, course_id, requester_id) VALUES ($1, $2, $3)", reviewPublish, id, viewer.ID); err != nil {
			return err
		}
		return setPublishState(ctx, tx, id, Pending)
	})
}

// WithdrawPublish takes back a pending publish request.
func (s *Service) WithdrawPublish(ctx context.Context, viewer auth.User, id int64) error {
	sm, err := s.loadVisible(ctx, viewer, id, "")
	if err != nil {
		return err
	}
	if !sm.IsUploader {
		return errNotUploader
	}
	if sm.Visibility != Private || sm.PublishState != Pending {
		return ErrInvalidState
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := settleRequests(ctx, tx, id, reviewPublish, reviewWithdrawn, nil); err != nil {
			return err
		}
		return setPublishState(ctx, tx, id, Unpublished)
	})
}

// Review is a request waiting for an admin.
type Review struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Course    Summary   `json:"course"`
	Requester Requester `json:"requester"`
	CreatedAt time.Time `json:"createdAt"`
	// Changes compares an update request's lessons with the course's.
	Changes *Changes `json:"changes,omitempty"`
	// Stale marks an update request submitted before the course last
	// changed: it should be reviewed against the new content.
	Stale bool `json:"stale"`
}

type Requester struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Reviews lists the pending requests, oldest first.
func (s *Service) Reviews(ctx context.Context, admin auth.User) ([]Review, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.kind, r.created_at, u.id, u.username, coalesce(r.base_dir, ''), r.manifest, `+summaryColumns("$1")+`
		FROM review_requests r JOIN users u ON u.id = r.requester_id JOIN courses c ON c.id = r.course_id
		WHERE r.state = 'pending' ORDER BY r.created_at, r.id`, admin.ID)
	if err != nil {
		return nil, err
	}
	type row struct {
		Review
		base     string
		manifest []byte
	}
	list, err := pgx.CollectRows(rows, func(cr pgx.CollectableRow) (row, error) {
		var r row
		err := cr.Scan(append([]any{&r.ID, &r.Kind, &r.CreatedAt, &r.Requester.ID, &r.Requester.Username, &r.base, &r.manifest}, summaryFields(&r.Course)...)...)
		r.Course.IsUploader = r.Course.UploaderID == admin.ID
		return r, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Review, 0, len(list))
	for _, r := range list {
		if r.Kind == reviewUpdate {
			r.Stale = r.base != r.Course.FilesDir
			m, _ := ParseManifest(r.manifest)
			if r.Changes, err = s.changes(ctx, r.Course.ID, m); err != nil {
				return nil, err
			}
		}
		out = append(out, r.Review)
	}
	return out, nil
}

// reviewRow is a pending request locked for deciding it.
type reviewRow struct {
	kind        string
	courseID    int64
	requesterID int64
	filesDir    string // an update request's package
	manifest    []byte
}

func pendingReview(ctx context.Context, tx pgx.Tx, id int64) (reviewRow, error) {
	var r reviewRow
	err := tx.QueryRow(ctx, `
		SELECT kind, course_id, requester_id, coalesce(files_dir, ''), manifest
		FROM review_requests WHERE id = $1 AND state = 'pending' FOR UPDATE`, id).
		Scan(&r.kind, &r.courseID, &r.requesterID, &r.filesDir, &r.manifest)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrReviewNotFound
	}
	return r, err
}

// Approve grants a pending request: a publish request puts the course into
// the library; an update request replaces the course's content and credits
// a requester who is not the uploader as a contributor.
func (s *Service) Approve(ctx context.Context, admin auth.User, reviewID int64) error {
	var drop struct {
		courseID int64
		dir      string
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		r, err := pendingReview(ctx, tx, reviewID)
		if err != nil {
			return err
		}
		if err := decide(ctx, tx, reviewID, reviewApproved, "", &admin); err != nil {
			return err
		}
		if r.kind == reviewPublish {
			drop.courseID = r.courseID
			if drop.dir, err = publish(ctx, tx, r.courseID); err != nil {
				return err
			}
			return notifyCourse(ctx, tx, r.requesterID, notify.PublishApproved, r.courseID, "")
		}
		var uploader int64
		if err := tx.QueryRow(ctx, "SELECT uploader_id, files_dir FROM courses WHERE id = $1 FOR UPDATE", r.courseID).Scan(&uploader, &drop.dir); err != nil {
			return err
		}
		m, problems := ParseManifest(r.manifest)
		if len(problems) > 0 {
			return &InvalidPackage{problems}
		}
		before, err := lessonFiles(ctx, tx, r.courseID)
		if err != nil {
			return err
		}
		diff, err := s.diffContent(ctx, r.courseID, before, drop.dir, m, r.filesDir)
		if err != nil {
			return err
		}
		if err := switchContent(ctx, tx, r.courseID, m, r.filesDir); err != nil {
			return err
		}
		if err := carryAnnotations(ctx, tx, r.courseID, diff); err != nil {
			return err
		}
		drop.courseID = r.courseID
		if err := notifyCourse(ctx, tx, r.requesterID, notify.UpdateApproved, r.courseID, ""); err != nil {
			return err
		}
		if err := notifyUpdated(ctx, tx, r.courseID, r.requesterID, diff.summary()); err != nil {
			return err
		}
		if r.requesterID == uploader {
			return nil
		}
		_, err = tx.Exec(ctx, "INSERT INTO course_contributors (course_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", r.courseID, r.requesterID)
		return err
	})
	if err == nil && drop.courseID != 0 {
		s.dropUnused(ctx, drop.courseID, drop.dir)
	}
	return err
}

// Reject turns a pending request down; the reason is shown to the requester.
func (s *Service) Reject(ctx context.Context, admin auth.User, reviewID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	var r reviewRow
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		if r, err = pendingReview(ctx, tx, reviewID); err != nil {
			return err
		}
		if err := decide(ctx, tx, reviewID, reviewRejected, reason, &admin); err != nil {
			return err
		}
		if r.kind == reviewPublish {
			if err := setPublishState(ctx, tx, r.courseID, Rejected); err != nil {
				return err
			}
			return notifyCourse(ctx, tx, r.requesterID, notify.PublishRejected, r.courseID, reason)
		}
		return notifyCourse(ctx, tx, r.requesterID, notify.UpdateRejected, r.courseID, reason)
	})
	if err == nil && r.filesDir != "" {
		s.dropFiles(ctx, r.courseID, r.filesDir)
	}
	return err
}

// Publish puts any private course into the library without a request,
// settling a pending publish request if there is one.
func (s *Service) Publish(ctx context.Context, admin auth.User, id int64) error {
	sm, err := s.load(ctx, admin, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Private {
		return ErrInvalidState
	}
	var frozen string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var requester int64
		err := tx.QueryRow(ctx, `
			UPDATE review_requests SET state = 'approved', decided_at = now(), decided_by = $2
			WHERE course_id = $1 AND kind = 'publish' AND state = 'pending' RETURNING requester_id`, id, admin.ID).Scan(&requester)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if frozen, err = publish(ctx, tx, id); err != nil {
			return err
		}
		if requester == 0 {
			return nil // nobody asked, so nobody is told
		}
		return notifyCourse(ctx, tx, requester, notify.PublishApproved, id, "")
	})
	if err == nil {
		s.dropUnused(ctx, id, frozen)
	}
	return err
}

// Unpublish takes a course out of the library; it becomes the uploader's
// private course again. Its files and structure are frozen for subscribers,
// who keep the lessons they finished as retained lessons. Pending update
// requests are turned down, since the course is no longer anyone's but the
// uploader's to change.
func (s *Service) Unpublish(ctx context.Context, admin auth.User, id int64) error {
	sm, err := s.load(ctx, admin, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Public {
		return ErrInvalidState
	}
	const reason = "课程已从课程库下架"
	var settled []turnedDown
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE courses SET visibility = 'private', publish_state = 'unpublished', updated_at = now() WHERE id = $1", id); err != nil {
			return err
		}
		if _, err := freeze(ctx, tx, id, RetentionUnpublished); err != nil {
			return err
		}
		if settled, err = settleLeaving(ctx, tx, id, reviewUpdate, reason, admin.ID); err != nil {
			return err
		}
		subs, err := subscribers(ctx, tx, id)
		if err != nil {
			return err
		}
		told := append([]int64{sm.UploaderID}, subs...)
		same := func(p notify.CoursePayload, _ int64) (any, error) { return p, nil }
		return notifyLeaving(ctx, tx, id, admin.ID, notify.CourseUnpublished, told, same, settled, reason)
	})
	if err != nil {
		return err
	}
	for _, r := range settled {
		if r.filesDir != "" {
			s.dropFiles(ctx, id, r.filesDir)
		}
	}
	return nil
}

// Edit changes a course's information; nil fields stay as they are.
type Edit struct {
	Title       *string `json:"title"`
	Intro       *string `json:"intro"`
	AuthorName  *string `json:"authorName"`
	AuthorIntro *string `json:"authorIntro"`
	Completion  *string `json:"completion"`
}

// EditInfo lets an admin correct a published course's information.
func (s *Service) EditInfo(ctx context.Context, admin auth.User, id int64, e Edit) error {
	if e.Title != nil {
		t := strings.TrimSpace(*e.Title)
		if t == "" || utf8.RuneCountInString(t) > maxTitleLength {
			return ErrBadInput
		}
		e.Title = &t
	}
	if e.Completion != nil && *e.Completion != Finished && *e.Completion != Ongoing && *e.Completion != Incomplete {
		return ErrBadInput
	}
	sm, err := s.load(ctx, admin, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Public {
		return ErrInvalidState // a private course is its uploader's alone
	}
	_, err = s.db.Exec(ctx, `
		UPDATE courses SET
			title = coalesce($2, title), intro = coalesce($3, intro), author_name = coalesce($4, author_name),
			author_intro = coalesce($5, author_intro), completion = coalesce($6, completion), updated_at = now()
		WHERE id = $1`, id, e.Title, e.Intro, e.AuthorName, e.AuthorIntro, e.Completion)
	return err
}

// notifyCourse tells a user about one of their requests on a course; a
// non-empty reason makes it a rejection.
func notifyCourse(ctx context.Context, tx pgx.Tx, userID int64, kind notify.Kind, courseID int64, reason string) error {
	var p notify.CoursePayload
	p.CourseID = courseID
	if err := tx.QueryRow(ctx, "SELECT title FROM courses WHERE id = $1", courseID).Scan(&p.CourseTitle); err != nil {
		return err
	}
	if reason != "" {
		return notify.Send(ctx, tx, userID, kind, notify.RejectedPayload{CoursePayload: p, Reason: reason})
	}
	return notify.Send(ctx, tx, userID, kind, p)
}

// publish puts a course into the library and ends its freeze, if any:
// subscribers read it as it is again. It returns the folder the freeze held
// ("" if none), for dropUnused once the transaction commits.
func publish(ctx context.Context, tx pgx.Tx, courseID int64) (string, error) {
	if _, err := tx.Exec(ctx, "UPDATE courses SET visibility = 'public', publish_state = 'unpublished', updated_at = now() WHERE id = $1", courseID); err != nil {
		return "", err
	}
	return thaw(ctx, tx, courseID)
}

func setPublishState(ctx context.Context, tx pgx.Tx, courseID int64, state string) error {
	_, err := tx.Exec(ctx, "UPDATE courses SET publish_state = $2 WHERE id = $1", courseID, state)
	return err
}

func decide(ctx context.Context, tx pgx.Tx, reviewID int64, state, reason string, by *auth.User) error {
	var byID *int64
	if by != nil {
		byID = &by.ID
	}
	_, err := tx.Exec(ctx, "UPDATE review_requests SET state = $2, reason = $3, decided_at = now(), decided_by = $4 WHERE id = $1",
		reviewID, state, reason, byID)
	return err
}

// settleRequests decides every pending request of one kind for a course.
func settleRequests(ctx context.Context, tx pgx.Tx, courseID int64, kind, state string, by *auth.User) error {
	var byID *int64
	if by != nil {
		byID = &by.ID
	}
	_, err := tx.Exec(ctx, `
		UPDATE review_requests SET state = $3, decided_at = now(), decided_by = $4
		WHERE course_id = $1 AND kind = $2 AND state = 'pending'`, courseID, kind, state, byID)
	return err
}

func (s *Service) tx(ctx context.Context, f func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
