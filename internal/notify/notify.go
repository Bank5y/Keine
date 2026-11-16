// Package notify keeps each user's notifications: in-site messages about
// their requests and the courses they follow. A notification is a kind plus
// a kind-specific JSON payload; the web app renders each kind.
//
// Adding a kind: declare its Kind and payload struct below, call Send where
// the event happens (inside the same transaction), and teach the web app's
// notification renderer the kind.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("notification not found")

type Kind string

const (
	// PublishApproved tells an uploader their course joined the library.
	PublishApproved Kind = "publish_approved"
	// PublishRejected tells an uploader their publish request was turned
	// down, and why.
	PublishRejected Kind = "publish_rejected"
	// UpdateApproved tells a requester their update replaced the course's
	// content.
	UpdateApproved Kind = "update_approved"
	// UpdateRejected tells a requester their update request was turned
	// down, and why.
	UpdateRejected Kind = "update_rejected"
	// CourseUpdated tells a subscriber a course's content was replaced, and
	// how many lessons that added, removed and changed.
	CourseUpdated Kind = "course_updated"
	// CourseUnpublished tells the uploader and subscribers a course left the
	// library.
	CourseUnpublished Kind = "course_unpublished"
	// CourseDeleted tells a subscriber a course was deleted, and how many of
	// its lessons they retain.
	CourseDeleted Kind = "course_deleted"
	// AnnotationsPending tells an annotator that a course update left some
	// of their annotations awaiting annotation migration, and how many.
	AnnotationsPending Kind = "annotations_pending"
	// PublishRequested tells every admin someone asked for a course to join
	// the library; it is marked read once the request is decided.
	PublishRequested Kind = "publish_requested"
	// UpdateRequested tells every admin someone submitted an update request;
	// it is marked read once the request is decided or withdrawn.
	UpdateRequested Kind = "update_requested"
)

// sticky kinds stay in the inbox after they are read, until dismissed.
var sticky = []string{string(AnnotationsPending)}

// RequestPayload is a request waiting for an admin.
type RequestPayload struct {
	CoursePayload
	Requester string `json:"requester"`
	RequestID int64  `json:"requestId"`
}

// PendingPayload counts the annotations, and the lessons they are in, that
// one course update left awaiting migration for a user; LessonID is the
// first of those lessons in course order.
type PendingPayload struct {
	CoursePayload
	Lessons     int   `json:"lessons"`
	Annotations int   `json:"annotations"`
	LessonID    int64 `json:"lessonId"`
}

// UpdatedPayload counts the lessons an update added, removed, modified
// (kept lessons whose text changed), renamed and moved (to another chapter,
// or out of their order).
type UpdatedPayload struct {
	CoursePayload
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Modified int `json:"modified"`
	Renamed  int `json:"renamed"`
	Moved    int `json:"moved"`
}

// DeletedPayload is a deleted course with the lessons the subscriber keeps.
type DeletedPayload struct {
	CoursePayload
	RetainedCount int `json:"retainedCount"`
}

// CoursePayload names the course a notification is about, as it was then.
type CoursePayload struct {
	CourseID    int64  `json:"courseId"`
	CourseTitle string `json:"courseTitle"`
}

// RejectedPayload is a turned-down request with the admin's reason.
type RejectedPayload struct {
	CoursePayload
	Reason string `json:"reason"`
}

// Execer is a pool or a transaction, so a notification is written together
// with the change it reports.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Send records a notification for a user.
func Send(ctx context.Context, db Execer, userID int64, kind Kind, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, "INSERT INTO notifications (user_id, kind, payload) VALUES ($1, $2, $3)", userID, string(kind), b)
	return err
}

// SendToAdmins records the same notification for every admin but except
// (the one who caused it, who needs no telling).
func SendToAdmins(ctx context.Context, db Execer, except int64, kind Kind, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, "INSERT INTO notifications (user_id, kind, payload) SELECT id, $1, $2 FROM users WHERE role = 'admin' AND id <> $3", string(kind), b, except)
	return err
}

type Notification struct {
	ID      int64           `json:"id"`
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Read    bool            `json:"read"`
	// Sticky is set while the notification stays in the inbox until
	// dismissed, read or not.
	Sticky    bool      `json:"sticky"`
	CreatedAt time.Time `json:"createdAt"`
}

type Page struct {
	Notifications []Notification `json:"notifications"`
	Total         int            `json:"total"`
	Unread        int            `json:"unread"`
	Page          int            `json:"page"`
	PageSize      int            `json:"pageSize"`
}

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// List returns one page of a user's notifications, newest first; page
// counts from 1. The inbox (what the bell drops down) holds only the unread
// ones and the sticky ones not yet dismissed, sticky first.
func (s *Service) List(ctx context.Context, userID int64, page, size int, inbox bool) (Page, error) {
	if size <= 0 {
		size = DefaultPageSize
	}
	size = min(size, MaxPageSize)
	page = max(page, 1)
	p := Page{Page: page, PageSize: size}
	const stuck = "(kind = ANY($2) AND dismissed_at IS NULL)"
	where := "user_id = $1 AND ($3 = false OR read_at IS NULL OR " + stuck + ")"
	if err := s.db.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE read_at IS NULL) FROM notifications WHERE "+where, userID, sticky, inbox).
		Scan(&p.Total, &p.Unread); err != nil {
		return p, err
	}
	order := "id DESC"
	if inbox {
		order = stuck + " DESC, id DESC"
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, kind, payload, read_at IS NOT NULL, `+stuck+`, created_at FROM notifications
		WHERE `+where+` ORDER BY `+order+` LIMIT $4 OFFSET $5`, userID, sticky, inbox, size, (page-1)*size)
	if err != nil {
		return p, err
	}
	p.Notifications, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, r.Scan(&n.ID, &n.Kind, &n.Payload, &n.Read, &n.Sticky, &n.CreatedAt)
	})
	if p.Notifications == nil {
		p.Notifications = []Notification{}
	}
	return p, err
}

func (s *Service) Unread(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, "SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL", userID).Scan(&n)
	return n, err
}

// MarkRead marks one of the user's notifications read; marking it again
// is harmless.
func (s *Service) MarkRead(ctx context.Context, userID, id int64) error {
	tag, err := s.db.Exec(ctx, "UPDATE notifications SET read_at = coalesce(read_at, now()) WHERE id = $1 AND user_id = $2", id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Dismiss takes one of the user's notifications out of their inbox for
// good, reading it too.
func (s *Service) Dismiss(ctx context.Context, userID, id int64) error {
	tag, err := s.db.Exec(ctx, "UPDATE notifications SET read_at = coalesce(read_at, now()), dismissed_at = coalesce(dismissed_at, now()) WHERE id = $1 AND user_id = $2", id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL", userID)
	return err
}
