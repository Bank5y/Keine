package course

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
)

// AnnotationState is where an annotation stands after course updates (see
// migration 0013): an attached annotation shows in the lesson text; a
// pending one awaits annotation migration after its text changed; changed
// and deleted ones are excerpts, kept after their text changed or their
// lesson was deleted.
type AnnotationState string

const (
	AnnotationAttached AnnotationState = "attached"
	AnnotationPending  AnnotationState = "pending"
	AnnotationChanged  AnnotationState = "changed"
	AnnotationDeleted  AnnotationState = "deleted"
)

// lessonChange is a kept lesson whose text an update changed.
type lessonChange struct {
	key      string
	old, new string // Markdown
}

// contentDiff is what replacing a course's lessons changes.
type contentDiff struct {
	added    int
	removed  []string // lesson keys
	modified []lessonChange
}

// summary is the diff as subscribers are told it.
func (d contentDiff) summary() notify.UpdatedPayload {
	return notify.UpdatedPayload{Added: d.added, Removed: len(d.removed), Modified: len(d.modified)}
}

// diffContent compares a course's lessons (before, with their files in
// oldDir) with manifest m (files in newDir): a kept lesson is modified when
// its text changed. Upcoming lessons have no content: one that gets content
// is added, one whose content is taken away is removed, and coming or going
// as upcoming changes nothing.
func (s *Service) diffContent(ctx context.Context, courseID int64, before map[string]currentLesson, oldDir string, m Manifest, newDir string) (contentDiff, error) {
	var d contentDiff
	incoming := map[string]bool{}
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			incoming[l.Key] = true
			prev, kept := before[l.Key]
			hadContent := kept && !prev.upcoming
			switch {
			case l.Upcoming():
				if hadContent {
					d.removed = append(d.removed, l.Key)
				}
				continue
			case !hadContent:
				d.added++
				continue
			}
			oldFile := prev.file
			old, err := s.lessonMarkdown(ctx, courseID, oldDir, oldFile)
			if err != nil {
				return d, err
			}
			next, err := s.lessonMarkdown(ctx, courseID, newDir, l.File)
			if err != nil {
				return d, err
			}
			if old != next || (oldFile == "") != (l.File == "") {
				d.modified = append(d.modified, lessonChange{key: l.Key, old: old, new: next})
			}
		}
	}
	for k, prev := range before {
		if !incoming[k] && !prev.upcoming {
			d.removed = append(d.removed, k)
		}
	}
	return d, nil
}

// lessonMarkdown reads a lesson's text from a folder of course files; a
// lesson without a text file has "".
func (s *Service) lessonMarkdown(ctx context.Context, courseID int64, dir, file string) (string, error) {
	if file == "" {
		return "", nil
	}
	b, err := s.readFile(ctx, filesPrefix(courseID, dir)+file)
	return string(b), err
}

// carryAnnotations brings the course's annotations through an update
// (ADR-0003): it keeps the text each modified lesson had, puts every
// annotation on it back where its quote is found with confidence and marks
// the others pending, and turns the annotations of deleted lessons into
// excerpts. Every changed text is kept, an empty one too. Each user with annotations it newly marked pending is told so,
// once (ticket #38).
func carryAnnotations(ctx context.Context, tx pgx.Tx, courseID int64, d contentDiff) error {
	if len(d.removed) > 0 {
		if _, err := tx.Exec(ctx, "UPDATE annotations SET state = $3 WHERE course_id = $1 AND lesson_key = ANY($2)",
			courseID, d.removed, AnnotationDeleted); err != nil {
			return err
		}
	}
	var pending []int64
	for _, ch := range d.modified {
		var prev int64
		if err := tx.QueryRow(ctx, "INSERT INTO previous_texts (course_id, lesson_key, markdown) VALUES ($1, $2, $3) RETURNING id",
			courseID, ch.key, ch.old).Scan(&prev); err != nil {
			return err
		}
		ids, err := relocate(ctx, tx, courseID, ch.key, anchor.MarkdownText(ch.new), prev)
		if err != nil {
			return err
		}
		pending = append(pending, ids...)
	}
	if err := clearPendingNotices(ctx, tx, courseID, 0); err != nil {
		return err
	}
	return notifyPending(ctx, tx, courseID, pending)
}

// notifyPending tells the owners of annotations an update just marked
// pending how many await migration and in how many lessons.
func notifyPending(ctx context.Context, tx pgx.Tx, courseID int64, pending []int64) error {
	if len(pending) == 0 {
		return nil
	}
	cp, err := coursePayload(ctx, tx, courseID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT a.user_id, count(DISTINCT a.lesson_key), count(*),
			(array_agg(l.id ORDER BY ch.position, l.position))[1]
		FROM annotations a
		JOIN lessons l ON l.course_id = a.course_id AND l.key = a.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE a.id = ANY($1)
		GROUP BY a.user_id ORDER BY a.user_id`, pending)
	if err != nil {
		return err
	}
	type told struct {
		user int64
		p    notify.PendingPayload
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (told, error) {
		t := told{p: notify.PendingPayload{CoursePayload: cp}}
		err := r.Scan(&t.user, &t.p.Lessons, &t.p.Annotations, &t.p.LessonID)
		return t, err
	})
	if err != nil {
		return err
	}
	for _, t := range list {
		if err := notify.Send(ctx, tx, t.user, notify.AnnotationsPending, t.p); err != nil {
			return err
		}
	}
	return nil
}

// relocate finds the annotations shown in, or awaiting, a lesson again in
// its new text. One that cannot be put back is pending against prev, the
// text it was shown in; one already pending keeps the text it was made on.
// It returns the annotations it newly marked pending.
func relocate(ctx context.Context, tx pgx.Tx, courseID int64, key string, text anchor.Text, prev int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, state, quote, prefix, suffix FROM annotations
		WHERE course_id = $1 AND lesson_key = $2 AND state IN ($3, $4)`, courseID, key, AnnotationAttached, AnnotationPending)
	if err != nil {
		return nil, err
	}
	type found struct {
		id    int64
		state AnnotationState
		q     anchor.Quote
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		err := r.Scan(&f.id, &f.state, &f.q.Exact, &f.q.Prefix, &f.q.Suffix)
		return f, err
	})
	if err != nil {
		return nil, err
	}
	var pending []int64
	for _, f := range list {
		var err error
		switch _, ok := text.Relocate(f.q); {
		case ok:
			_, err = tx.Exec(ctx, "UPDATE annotations SET state = $2, previous_text_id = NULL WHERE id = $1", f.id, AnnotationAttached)
		case f.state == AnnotationAttached:
			_, err = tx.Exec(ctx, "UPDATE annotations SET state = $2, previous_text_id = $3 WHERE id = $1", f.id, AnnotationPending, prev)
			pending = append(pending, f.id)
		}
		if err != nil {
			return nil, err
		}
	}
	return pending, nil
}

// PreviousText is the text a lesson had before a course update changed it.
type PreviousText struct {
	ID         int64     `json:"id"`
	Markdown   string    `json:"markdown"`
	ReplacedAt time.Time `json:"replacedAt"`
}

// PreviousTexts lists the earlier texts of a lesson the viewer may read,
// newest first.
func (s *Service) PreviousTexts(ctx context.Context, viewer auth.User, courseID, lessonID int64) ([]PreviousText, error) {
	l, err := s.Lesson(ctx, viewer, courseID, lessonID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, markdown, replaced_at FROM previous_texts
		WHERE course_id = $1 AND lesson_key = $2 ORDER BY replaced_at DESC, id DESC`, courseID, l.Key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PreviousText, error) {
		var p PreviousText
		err := r.Scan(&p.ID, &p.Markdown, &p.ReplacedAt)
		return p, err
	})
}
