package course

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// Why a subscriber reads a course from its freeze (ADR-0004).
const (
	RetentionUnpublished = "unpublished"
)

// snapshot is a course's information and structure at the moment it was
// frozen; retained lessons are read from it and the frozen files.
type snapshot struct {
	Title       string        `json:"title"`
	Type        string        `json:"type"`
	Completion  string        `json:"completion"`
	Intro       string        `json:"intro"`
	AuthorName  string        `json:"authorName"`
	AuthorIntro string        `json:"authorIntro"`
	Cover       string        `json:"cover"`
	Chapters    []snapChapter `json:"chapters"`
}

type snapChapter struct {
	ID      int64        `json:"id"`
	Key     string       `json:"key"`
	Title   string       `json:"title"`
	Lessons []snapLesson `json:"lessons"`
}

type snapLesson struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	File      string `json:"file"`
	VideoFile string `json:"videoFile"`
	VideoURL  string `json:"videoUrl"`
	Duration  string `json:"duration"`
}

// retention is what one subscriber keeps of a course that left the library:
// its freeze and the lessons they retained, plus their done lessons.
type retention struct {
	reason   string
	filesDir string
	snap     snapshot
	retained map[string]bool
	done     map[string]bool
}

// find returns the first lesson of the frozen structure matching, with its
// chapter's id.
func (r *retention) find(match func(snapLesson) bool) (snapLesson, int64, bool) {
	for _, ch := range r.snap.Chapters {
		for _, l := range ch.Lessons {
			if match(l) {
				return l, ch.ID, true
			}
		}
	}
	return snapLesson{}, 0, false
}

// retainedLesson finds a lesson the subscriber may still read, by id.
func (r *retention) retainedLesson(id int64) (snapLesson, int64, bool) {
	return r.find(func(l snapLesson) bool { return l.ID == id && r.retained[l.Key] })
}

// canRead tells whether a frozen file may be served: everything but the
// text and video of lessons the subscriber did not retain.
func (r *retention) canRead(path string) bool {
	hidden := false
	for _, ch := range r.snap.Chapters {
		for _, l := range ch.Lessons {
			if l.File == path || l.VideoFile == path {
				if r.retained[l.Key] {
					return true
				}
				hidden = true
			}
		}
	}
	return !hidden
}

// apply makes a course summary show the frozen course.
func (r *retention) apply(sm *Summary) {
	sm.Title, sm.Type, sm.Completion, sm.AuthorName = r.snap.Title, r.snap.Type, r.snap.Completion, r.snap.AuthorName
	sm.HasCover = r.snap.Cover != ""
	sm.FilesDir = r.filesDir
	sm.LessonCount, sm.DoneCount = 0, 0
	for _, ch := range r.snap.Chapters {
		for _, l := range ch.Lessons {
			sm.LessonCount++
			if r.done[l.Key] {
				sm.DoneCount++
			}
		}
	}
	sm.Retention = r.reason
	sm.RetainedCount = len(r.retained)
}

// structure is the frozen table of contents; lessons not retained are
// unavailable.
func (r *retention) structure() []ChapterDetail {
	out := []ChapterDetail{}
	for _, ch := range r.snap.Chapters {
		c := ChapterDetail{ID: ch.ID, Key: ch.Key, Title: ch.Title, Lessons: []LessonDetail{}}
		for _, l := range ch.Lessons {
			d := LessonDetail{ID: l.ID, Key: l.Key, Title: l.Title, Duration: l.Duration, HasText: l.File != "",
				State: StateUnstarted, Unavailable: !r.retained[l.Key]}
			switch {
			case l.VideoFile != "":
				d.Video = "file"
			case l.VideoURL != "":
				d.Video = "url"
			}
			if r.done[l.Key] {
				d.State = StateDone
				c.Done++
			}
			c.Lessons = append(c.Lessons, d)
		}
		out = append(out, c)
	}
	return out
}

// open loads a course for reading: as it is, for viewers who may see it,
// or as frozen, for a subscriber who retains lessons of it. Anyone else
// gets ErrNotFound. The retention is nil when the course is read as it is.
func (s *Service) open(ctx context.Context, viewer auth.User, id int64, extraColumns string, extra ...any) (Summary, *retention, error) {
	sm, err := s.load(ctx, viewer, id, extraColumns, extra...)
	if err != nil {
		return Summary{}, nil, err
	}
	if canView(viewer, sm) {
		return sm, nil, nil
	}
	r, err := s.retentionOf(ctx, viewer, id)
	if err != nil {
		return Summary{}, nil, err
	}
	if r == nil {
		return Summary{}, nil, ErrNotFound
	}
	r.apply(&sm)
	return sm, r, nil
}

// retentionOf returns what the viewer, as a subscriber, retains of a frozen
// course; nil if nothing.
func (s *Service) retentionOf(ctx context.Context, viewer auth.User, courseID int64) (*retention, error) {
	r := retention{retained: map[string]bool{}, done: map[string]bool{}}
	var raw []byte
	var keys []string
	err := s.db.QueryRow(ctx, `
		SELECT f.reason, f.files_dir, f.snapshot, array_agg(rl.lesson_key)
		FROM course_freezes f
		JOIN retained_lessons rl ON rl.course_id = f.course_id AND rl.user_id = $2
		JOIN course_readers cr ON cr.course_id = f.course_id AND cr.user_id = $2 AND cr.subscribed_at IS NOT NULL
		WHERE f.course_id = $1
		GROUP BY f.course_id`, courseID, viewer.ID).Scan(&r.reason, &r.filesDir, &raw, &keys)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &r.snap); err != nil {
		return nil, err
	}
	for _, k := range keys {
		r.retained[k] = true
	}
	rows, err := s.db.Query(ctx, "SELECT lesson_key FROM lesson_progress WHERE user_id = $1 AND course_id = $2 AND done", viewer.ID, courseID)
	if err != nil {
		return nil, err
	}
	done, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, k := range done {
		r.done[k] = true
	}
	return &r, nil
}

// freeze keeps a course's current files and structure for its subscribers
// as it leaves the library: each subscriber other than the uploader retains
// the lessons they finished. A course already frozen keeps its first freeze
// (and its retained lessons) and only takes the new reason. It reports
// whether anyone retains lessons; with nobody, no freeze is kept.
func freeze(ctx context.Context, tx pgx.Tx, courseID int64, reason string) (bool, error) {
	var existed bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM course_freezes WHERE course_id = $1)", courseID).Scan(&existed); err != nil {
		return false, err
	}
	if existed {
		_, err := tx.Exec(ctx, "UPDATE course_freezes SET reason = $2 WHERE course_id = $1", courseID, reason)
		return err == nil, err
	}
	snap, dir, err := takeSnapshot(ctx, tx, courseID)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO course_freezes (course_id, reason, files_dir, snapshot) VALUES ($1, $2, $3, $4)",
		courseID, reason, dir, raw); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO retained_lessons (course_id, user_id, lesson_key)
		SELECT p.course_id, p.user_id, p.lesson_key
		FROM lesson_progress p
		JOIN lessons l ON l.course_id = p.course_id AND l.key = p.lesson_key
		JOIN course_readers cr ON cr.course_id = p.course_id AND cr.user_id = p.user_id AND cr.subscribed_at IS NOT NULL
		JOIN courses c ON c.id = p.course_id
		WHERE p.course_id = $1 AND p.done AND p.user_id <> c.uploader_id`, courseID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	_, err = tx.Exec(ctx, "DELETE FROM course_freezes WHERE course_id = $1", courseID)
	return false, err
}

func takeSnapshot(ctx context.Context, tx pgx.Tx, courseID int64) (snapshot, string, error) {
	var snap snapshot
	var dir string
	if err := tx.QueryRow(ctx, `
		SELECT title, type, completion, intro, author_name, author_intro, coalesce(cover, ''), files_dir
		FROM courses WHERE id = $1`, courseID).
		Scan(&snap.Title, &snap.Type, &snap.Completion, &snap.Intro, &snap.AuthorName, &snap.AuthorIntro, &snap.Cover, &dir); err != nil {
		return snap, "", err
	}
	rows, err := tx.Query(ctx, `
		SELECT ch.id, ch.key, ch.title, l.id, l.key, l.title, coalesce(l.file, ''), coalesce(l.video_file, ''),
		       coalesce(l.video_url, ''), l.duration
		FROM chapters ch JOIN lessons l ON l.chapter_id = ch.id
		WHERE ch.course_id = $1
		ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return snap, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var ch snapChapter
		var l snapLesson
		if err := rows.Scan(&ch.ID, &ch.Key, &ch.Title, &l.ID, &l.Key, &l.Title, &l.File, &l.VideoFile, &l.VideoURL, &l.Duration); err != nil {
			return snap, "", err
		}
		if len(snap.Chapters) == 0 || snap.Chapters[len(snap.Chapters)-1].ID != ch.ID {
			snap.Chapters = append(snap.Chapters, ch)
		}
		last := &snap.Chapters[len(snap.Chapters)-1]
		last.Lessons = append(last.Lessons, l)
	}
	return snap, dir, rows.Err()
}

// thaw ends a course's freeze, as the course is back in the library and
// every subscriber reads it as it is. It returns the frozen folder ("" if
// there was no freeze) for dropUnused once the transaction commits.
func thaw(ctx context.Context, tx pgx.Tx, courseID int64) (string, error) {
	var dir string
	err := tx.QueryRow(ctx, "DELETE FROM course_freezes WHERE course_id = $1 RETURNING files_dir", courseID).Scan(&dir)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return dir, err
}

// releaseRetainer takes a user's retained lessons of a course away. When
// nobody retains anything any more the freeze ends: the frozen folder is
// returned ("" while the freeze stays) for dropUnused after commit.
func releaseRetainer(ctx context.Context, tx pgx.Tx, courseID, userID int64) (string, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM retained_lessons WHERE course_id = $1 AND user_id = $2", courseID, userID); err != nil {
		return "", err
	}
	var left bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM retained_lessons WHERE course_id = $1)", courseID).Scan(&left); err != nil || left {
		return "", err
	}
	return thaw(ctx, tx, courseID)
}

// dropUnused removes a folder of course files unless the course still uses
// it or a freeze holds it.
func (s *Service) dropUnused(ctx context.Context, courseID int64, dir string) {
	if dir == "" {
		return
	}
	var used bool
	if err := s.db.QueryRow(context.WithoutCancel(ctx), `
		SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND files_dir = $2)
		    OR EXISTS (SELECT 1 FROM course_freezes WHERE course_id = $1 AND files_dir = $2)`, courseID, dir).Scan(&used); err != nil || used {
		return
	}
	s.dropFiles(ctx, courseID, dir)
}
