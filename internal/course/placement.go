package course

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// ErrNotPending: the annotation does not await annotation migration.
var ErrNotPending = errors.New("annotation not pending")

// Placement is where in a lesson's text, as it is now, an annotation goes:
// the quoted text and a little text around it, as a selection gives them.
type Placement struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// ownAnnotation loads one of the viewer's annotations with the lesson it is on.
func (s *Service) ownAnnotation(ctx context.Context, viewer auth.User, id int64) (a Annotation, courseID int64, key string, err error) {
	err = scanAnnotation(s.db.QueryRow(ctx, "SELECT course_id, lesson_key, "+annotationColumns+" FROM annotations WHERE id = $1 AND user_id = $2", id, viewer.ID),
		&a, &courseID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAnnotationNotFound
	}
	return a, courseID, key, err
}

// lessonIDByKey finds the lesson a key names in a course the viewer may
// read, or in what he retains of a frozen one.
func (s *Service) lessonIDByKey(ctx context.Context, viewer auth.User, courseID int64, key string) (int64, error) {
	_, ret, err := s.open(ctx, viewer, courseID, "")
	if err != nil {
		return 0, err
	}
	if ret != nil {
		l, _, ok := ret.find(func(l snapLesson) bool { return l.Key == key && ret.retained[key] })
		if !ok {
			return 0, ErrLessonNotFound
		}
		return l.ID, nil
	}
	var id int64
	err = s.db.QueryRow(ctx, "SELECT id FROM lessons WHERE course_id = $1 AND key = $2", courseID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrLessonNotFound
	}
	return id, err
}

// Place puts one of the viewer's annotations that awaits migration, or was
// kept as an excerpt after its text changed, on a placement in the lesson's
// text as it is now (annotation migration). It keeps its look and note and
// comes back attached and located. Whatever picks the place (a selection, a
// suggestion, a drop) goes through here.
func (s *Service) Place(ctx context.Context, viewer auth.User, id int64, p Placement) (Annotation, error) {
	if !validQuote(p.Quote, p.Prefix, p.Suffix) {
		return Annotation{}, ErrBadInput
	}
	a, courseID, key, err := s.ownAnnotation(ctx, viewer, id)
	if err != nil {
		return Annotation{}, err
	}
	if a.State != AnnotationPending && a.State != AnnotationChanged {
		return Annotation{}, ErrNotPending
	}
	lessonID, err := s.lessonIDByKey(ctx, viewer, courseID, key)
	if err != nil {
		return Annotation{}, err
	}
	_, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if err != nil {
		return Annotation{}, err
	}
	moved := Annotation{Quote: p.Quote, Prefix: p.Prefix, Suffix: p.Suffix, State: AnnotationAttached}
	if locate(text, &moved); moved.Range == nil {
		return Annotation{}, ErrQuoteNotFound
	}
	err = scanAnnotation(s.db.QueryRow(ctx, `UPDATE annotations SET quote = $3, prefix = $4, suffix = $5, state = $6,
		previous_text_id = NULL, updated_at = now() WHERE id = $1 AND user_id = $2 RETURNING `+annotationColumns,
		id, viewer.ID, p.Quote, p.Prefix, p.Suffix, AnnotationAttached), &a)
	a.Range = moved.Range
	if err != nil {
		return a, err
	}
	return a, s.clearPendingNotice(ctx, viewer.ID, courseID)
}

// KeepAsExcerpt keeps one of the viewer's annotations that awaits migration
// as an excerpt: it leaves the text, its quote and note stay, and the notes
// page tells its text changed.
func (s *Service) KeepAsExcerpt(ctx context.Context, viewer auth.User, id int64) error {
	var courseID int64
	err := s.db.QueryRow(ctx, "UPDATE annotations SET state = $3, updated_at = now() WHERE id = $1 AND user_id = $2 AND state = $4 RETURNING course_id",
		id, viewer.ID, AnnotationChanged, AnnotationPending).Scan(&courseID)
	if err == nil {
		return s.clearPendingNotice(ctx, viewer.ID, courseID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, _, _, err := s.ownAnnotation(ctx, viewer, id); err != nil {
		return err
	}
	return ErrNotPending
}
