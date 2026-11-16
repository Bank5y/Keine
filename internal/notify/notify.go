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
)

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

type Notification struct {
	ID        int64           `json:"id"`
	Kind      Kind            `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	Read      bool            `json:"read"`
	CreatedAt time.Time       `json:"createdAt"`
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
// counts from 1.
func (s *Service) List(ctx context.Context, userID int64, page, size int) (Page, error) {
	if size <= 0 {
		size = DefaultPageSize
	}
	size = min(size, MaxPageSize)
	page = max(page, 1)
	p := Page{Page: page, PageSize: size}
	if err := s.db.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE read_at IS NULL) FROM notifications WHERE user_id = $1", userID).
		Scan(&p.Total, &p.Unread); err != nil {
		return p, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, kind, payload, read_at IS NOT NULL, created_at FROM notifications
		WHERE user_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`, userID, size, (page-1)*size)
	if err != nil {
		return p, err
	}
	p.Notifications, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, r.Scan(&n.ID, &n.Kind, &n.Payload, &n.Read, &n.CreatedAt)
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

func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, "UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL", userID)
	return err
}
