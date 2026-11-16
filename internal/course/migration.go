package course

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
)

// Annotation states (see migration 0013): an attached annotation shows in
// the lesson text; a pending one awaits annotation migration after its text
// changed; changed and deleted ones are excerpts, kept after their text
// changed or their lesson was deleted.
const (
	AnnotationAttached = "attached"
	AnnotationPending  = "pending"
	AnnotationChanged  = "changed"
	AnnotationDeleted  = "deleted"
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
// its text changed.
func (s *Service) diffContent(ctx context.Context, courseID int64, before map[string]string, oldDir string, m Manifest, newDir string) (contentDiff, error) {
	var d contentDiff
	incoming := map[string]bool{}
	for _, ch := range m.Chapters {
		for _, l := range ch.Lessons {
			incoming[l.Key] = true
			oldFile, kept := before[l.Key]
			if !kept {
				d.added++
				continue
			}
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
	for k := range before {
		if !incoming[k] {
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
// excerpts.
func carryAnnotations(ctx context.Context, tx pgx.Tx, courseID int64, d contentDiff) error {
	if len(d.removed) > 0 {
		if _, err := tx.Exec(ctx, "UPDATE annotations SET state = $3 WHERE course_id = $1 AND lesson_key = ANY($2)",
			courseID, d.removed, AnnotationDeleted); err != nil {
			return err
		}
	}
	for _, ch := range d.modified {
		var prev *int64
		if ch.old != "" {
			var id int64
			if err := tx.QueryRow(ctx, "INSERT INTO previous_texts (course_id, lesson_key, markdown) VALUES ($1, $2, $3) RETURNING id",
				courseID, ch.key, ch.old).Scan(&id); err != nil {
				return err
			}
			prev = &id
		}
		if err := relocate(ctx, tx, courseID, ch.key, anchor.MarkdownText(ch.new), prev); err != nil {
			return err
		}
	}
	return nil
}

// relocate finds the annotations shown in, or awaiting, a lesson again in
// its new text. One that cannot be put back is pending against prev, the
// text it was shown in; one already pending keeps the text it was made on.
func relocate(ctx context.Context, tx pgx.Tx, courseID int64, key string, text anchor.Text, prev *int64) error {
	rows, err := tx.Query(ctx, `SELECT id, state, quote, prefix, suffix FROM annotations
		WHERE course_id = $1 AND lesson_key = $2 AND state IN ($3, $4)`, courseID, key, AnnotationAttached, AnnotationPending)
	if err != nil {
		return err
	}
	type found struct {
		id    int64
		state string
		q     anchor.Quote
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		err := r.Scan(&f.id, &f.state, &f.q.Exact, &f.q.Prefix, &f.q.Suffix)
		return f, err
	})
	if err != nil {
		return err
	}
	for _, f := range list {
		var err error
		switch _, ok := text.Relocate(f.q); {
		case ok:
			_, err = tx.Exec(ctx, "UPDATE annotations SET state = $2, previous_text_id = NULL WHERE id = $1", f.id, AnnotationAttached)
		case f.state == AnnotationAttached:
			_, err = tx.Exec(ctx, "UPDATE annotations SET state = $2, previous_text_id = $3 WHERE id = $1", f.id, AnnotationPending, prev)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
