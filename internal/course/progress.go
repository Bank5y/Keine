package course

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// Lesson states, as the viewer sees a lesson.
const (
	StateUnstarted = "unstarted"
	StateStudying  = "studying"
	StateDone      = "done"
)

// lessonState is the SQL for a lesson's state from its lesson_progress row p.
const lessonState = `CASE WHEN p.done THEN 'done' WHEN p.lesson_key IS NOT NULL THEN 'studying' ELSE 'unstarted' END`

// ErrBadProgress rejects a progress update that makes no sense.
var ErrBadProgress = errors.New("invalid progress")

// lessonKey returns the stable key of a lesson of a course the viewer may see.
func (s *Service) lessonKey(ctx context.Context, viewer auth.User, courseID, lessonID int64) (string, error) {
	if _, err := s.loadVisible(ctx, viewer, courseID, ""); err != nil {
		return "", err
	}
	var key string
	err := s.db.QueryRow(ctx, "SELECT key FROM lessons WHERE id = $1 AND course_id = $2", lessonID, courseID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLessonNotFound
	}
	return key, err
}

// Visit records that the viewer opened a lesson: an unstarted lesson
// becomes studying, and it becomes the course's last studied lesson.
func (s *Service) Visit(ctx context.Context, viewer auth.User, courseID, lessonID int64) error {
	key, err := s.lessonKey(ctx, viewer, courseID, lessonID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO lesson_progress (user_id, course_id, lesson_key) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, viewer.ID, courseID, key); err != nil {
		return err
	}
	return s.touch(ctx, viewer, courseID, key)
}

// touch moves the course to the top of the viewer's courses.
func (s *Service) touch(ctx context.Context, viewer auth.User, courseID int64, key string) error {
	_, err := s.db.Exec(ctx, "UPDATE enrollments SET last_lesson_key = $3, last_studied_at = now() WHERE user_id = $1 AND course_id = $2",
		viewer.ID, courseID, key)
	return err
}

// ProgressUpdate changes a lesson's progress; nil fields stay as they are.
type ProgressUpdate struct {
	Done     *bool    `json:"done"`
	Position *float64 `json:"position"`
}

// SetProgress marks a lesson done or not done and/or remembers the
// position in it. Undoing done leaves the lesson studying.
func (s *Service) SetProgress(ctx context.Context, viewer auth.User, courseID, lessonID int64, u ProgressUpdate) error {
	if u.Position != nil && *u.Position < 0 {
		return ErrBadProgress
	}
	key, err := s.lessonKey(ctx, viewer, courseID, lessonID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO lesson_progress AS p (user_id, course_id, lesson_key, done, position)
		VALUES ($1, $2, $3, coalesce($4::boolean, false), coalesce($5::double precision, 0))
		ON CONFLICT (user_id, course_id, lesson_key) DO UPDATE SET
			done = coalesce($4::boolean, p.done),
			position = coalesce($5::double precision, p.position),
			updated_at = now()`, viewer.ID, courseID, key, u.Done, u.Position); err != nil {
		return err
	}
	return s.touch(ctx, viewer, courseID, key)
}
