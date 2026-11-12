package course

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
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
}

type Requester struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Reviews lists the pending requests, oldest first.
func (s *Service) Reviews(ctx context.Context, admin auth.User) ([]Review, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.kind, r.created_at, u.id, u.username, `+summaryColumns("$1")+`
		FROM review_requests r JOIN users u ON u.id = r.requester_id JOIN courses c ON c.id = r.course_id
		WHERE r.state = 'pending' ORDER BY r.created_at, r.id`, admin.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Review, error) {
		var r Review
		err := row.Scan(append([]any{&r.ID, &r.Kind, &r.CreatedAt, &r.Requester.ID, &r.Requester.Username}, summaryFields(&r.Course)...)...)
		r.Course.IsUploader = r.Course.UploaderID == admin.ID
		return r, err
	})
}

// pendingReview locks a pending request for deciding it.
func pendingReview(ctx context.Context, tx pgx.Tx, id int64) (kind string, courseID int64, err error) {
	err = tx.QueryRow(ctx, "SELECT kind, course_id FROM review_requests WHERE id = $1 AND state = 'pending' FOR UPDATE", id).Scan(&kind, &courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrReviewNotFound
	}
	return kind, courseID, err
}

// Approve grants a pending request.
func (s *Service) Approve(ctx context.Context, admin auth.User, reviewID int64) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		kind, courseID, err := pendingReview(ctx, tx, reviewID)
		if err != nil {
			return err
		}
		if kind != reviewPublish {
			return ErrInvalidState
		}
		if err := decide(ctx, tx, reviewID, reviewApproved, "", &admin); err != nil {
			return err
		}
		return publish(ctx, tx, courseID)
	})
}

// Reject turns a pending request down; the reason is shown to the requester.
func (s *Service) Reject(ctx context.Context, admin auth.User, reviewID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		kind, courseID, err := pendingReview(ctx, tx, reviewID)
		if err != nil {
			return err
		}
		if err := decide(ctx, tx, reviewID, reviewRejected, reason, &admin); err != nil {
			return err
		}
		if kind == reviewPublish {
			return setPublishState(ctx, tx, courseID, Rejected)
		}
		return nil
	})
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
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := settleRequests(ctx, tx, id, reviewPublish, reviewApproved, &admin); err != nil {
			return err
		}
		return publish(ctx, tx, id)
	})
}

// Unpublish takes a course out of the library; it becomes the uploader's
// private course again.
func (s *Service) Unpublish(ctx context.Context, admin auth.User, id int64) error {
	sm, err := s.load(ctx, admin, id, "")
	if err != nil {
		return err
	}
	if sm.Visibility != Public {
		return ErrInvalidState
	}
	_, err = s.db.Exec(ctx, "UPDATE courses SET visibility = 'private', publish_state = 'unpublished', updated_at = now() WHERE id = $1", id)
	return err
}

// Edit changes a course's information; nil fields stay as they are.
type Edit struct {
	Title       *string `json:"title"`
	Intro       *string `json:"intro"`
	AuthorName  *string `json:"authorName"`
	AuthorIntro *string `json:"authorIntro"`
	Completion  *string `json:"completion"`
}

// EditInfo lets an admin correct a course's information.
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
	if _, err := s.load(ctx, admin, id, ""); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `
		UPDATE courses SET
			title = coalesce($2, title), intro = coalesce($3, intro), author_name = coalesce($4, author_name),
			author_intro = coalesce($5, author_intro), completion = coalesce($6, completion), updated_at = now()
		WHERE id = $1`, id, e.Title, e.Intro, e.AuthorName, e.AuthorIntro, e.Completion)
	return err
}

func publish(ctx context.Context, tx pgx.Tx, courseID int64) error {
	_, err := tx.Exec(ctx, "UPDATE courses SET visibility = 'public', publish_state = 'unpublished', updated_at = now() WHERE id = $1", courseID)
	return err
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
