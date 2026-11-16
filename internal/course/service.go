package course

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/notify"
	"github.com/Bank5y/keine/internal/storage"
)

var (
	ErrNotFound  = errors.New("course not found")
	ErrForbidden = errors.New("not allowed")
)

const (
	Private = "private"
	Public  = "public"

	Unpublished = "unpublished"
	Pending     = "pending"
	Rejected    = "rejected"
)

type Service struct {
	db    *pgxpool.Pool
	files storage.Store
}

func NewService(db *pgxpool.Pool, files storage.Store) *Service {
	return &Service{db: db, files: files}
}

// coursePrefix holds every file of a course, across versions.
func coursePrefix(courseID int64) string { return fmt.Sprintf("courses/%d/", courseID) }

// filesPrefix holds one set of a course's files: the folder in use, a new
// version being written, or an update request's package.
func filesPrefix(courseID int64, dir string) string {
	return fmt.Sprintf("courses/%d/%s/", courseID, dir)
}

// newFilesDir names a fresh folder for a new version of a course's files.
func newFilesDir() string { return fmt.Sprintf("v%d", time.Now().UnixNano()) }

// parsedPackage is a course package that passed every check.
type parsedPackage struct {
	pkg      *pkg
	manifest Manifest
	raw      []byte // course.json as uploaded
	files    []string
}

// parsePackage checks a course package against its manifest; a package that
// cannot become a course is *InvalidPackage listing every problem.
func parsePackage(zr *zip.Reader) (parsedPackage, error) {
	p, problems := openPackage(zr)
	if problems != nil {
		return parsedPackage{}, &InvalidPackage{problems}
	}
	data, err := p.read(ManifestFile, maxManifestSize)
	if err != nil {
		return parsedPackage{}, &InvalidPackage{[]Problem{{ManifestFile, "无法读取: " + err.Error()}}}
	}
	m, problems := ParseManifest(data)
	if len(problems) > 0 {
		return parsedPackage{}, &InvalidPackage{problems}
	}
	files, problems := p.resolve(m)
	if len(problems) > 0 {
		return parsedPackage{}, &InvalidPackage{problems}
	}
	return parsedPackage{pkg: p, manifest: m, raw: data, files: files}, nil
}

// Import creates a private course from a course package, subscribed by the
// uploader so it stays in their courses once published. Either the whole course is created or nothing is: an invalid
// package returns *InvalidPackage, and a failure while storing removes what
// was already written.
func (s *Service) Import(ctx context.Context, uploader auth.User, zr *zip.Reader) (int64, error) {
	pp, err := parsePackage(zr)
	if err != nil {
		return 0, err
	}
	m := pp.manifest
	var id int64
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO courses (uploader_id, title, type, completion, intro, author_name, author_intro, cover)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
			RETURNING id`, uploader.ID, m.Title, m.Type, m.Completion, m.Intro, m.AuthorName, m.AuthorIntro, m.Cover).Scan(&id); err != nil {
			return err
		}
		if err := writeStructure(ctx, tx, id, m); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO course_readers (user_id, course_id, subscribed_at) VALUES ($1, $2, now())", uploader.ID, id); err != nil {
			return err
		}
		if err := s.storeFiles(ctx, pp, filesPrefix(id, "v1")); err != nil {
			_ = s.files.DeletePrefix(context.WithoutCancel(ctx), coursePrefix(id))
			return err
		}
		return nil
	})
	if err != nil && id != 0 {
		_ = s.files.DeletePrefix(context.WithoutCancel(ctx), coursePrefix(id))
	}
	return id, err
}

// writeStructure makes a course's chapters and lessons those of the
// manifest. Chapters and lessons are matched by key, so a lesson that stays
// keeps its id (and its links); the ones the manifest drops are removed.
// Learning records hang on lesson keys and are not touched.
func writeStructure(ctx context.Context, tx pgx.Tx, courseID int64, m Manifest) error {
	chapterKeys := []string{}
	lessonKeys := []string{}
	for i, ch := range m.Chapters {
		var chapterID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO chapters (course_id, key, title, position) VALUES ($1, $2, $3, $4)
			ON CONFLICT (course_id, key) DO UPDATE SET title = EXCLUDED.title, position = EXCLUDED.position
			RETURNING id`, courseID, ch.Key, ch.Title, i).Scan(&chapterID); err != nil {
			return err
		}
		chapterKeys = append(chapterKeys, ch.Key)
		for j, l := range ch.Lessons {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lessons (course_id, chapter_id, key, title, position, file, video_file, video_url, duration)
				VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9)
				ON CONFLICT (course_id, key) DO UPDATE SET
					chapter_id = EXCLUDED.chapter_id, title = EXCLUDED.title, position = EXCLUDED.position,
					file = EXCLUDED.file, video_file = EXCLUDED.video_file, video_url = EXCLUDED.video_url, duration = EXCLUDED.duration`,
				courseID, chapterID, l.Key, l.Title, j, l.File, l.VideoFile, l.VideoURL, l.Duration); err != nil {
				return err
			}
			lessonKeys = append(lessonKeys, l.Key)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM lessons WHERE course_id = $1 AND NOT (key = ANY($2))", courseID, lessonKeys); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM chapters WHERE course_id = $1 AND NOT (key = ANY($2))", courseID, chapterKeys)
	return err
}

func (s *Service) storeFiles(ctx context.Context, pp parsedPackage, dir string) error {
	for _, name := range pp.files {
		f := pp.pkg.files[name]
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", name, err)
		}
		err = s.files.Put(ctx, dir+name, rc, int64(f.UncompressedSize64))
		rc.Close()
		if err != nil {
			return fmt.Errorf("store %s: %w", name, err)
		}
	}
	return nil
}

// Summary is a course as shown on a card.
type Summary struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Type         string `json:"type"`
	Completion   string `json:"completion"`
	AuthorName   string `json:"authorName"`
	HasCover     bool   `json:"hasCover"`
	Visibility   string `json:"visibility"`
	PublishState string `json:"publishState"`
	UploaderID   int64  `json:"uploaderId"`
	IsUploader   bool   `json:"isUploader"`
	LessonCount  int    `json:"lessonCount"`
	// Subscribed tells whether the viewer subscribes to the course.
	Subscribed bool `json:"subscribed"`
	// DoneCount is how many lessons the viewer has finished.
	DoneCount int       `json:"doneCount"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Retention is set when the course left the library and the viewer, a
	// subscriber, reads only their retained lessons of it: "unpublished" or
	// "deleted".
	Retention RetentionReason `json:"retention,omitempty"`
	// RetainedCount is how many lessons the viewer retains, with Retention.
	RetainedCount int `json:"retainedCount,omitempty"`
	// FilesDir is the folder of the course's files the viewer reads.
	FilesDir string `json:"-"`
	// Deleted marks a course deleted while subscribers retain lessons of it:
	// only they reach it, through its freeze.
	Deleted bool `json:"-"`
}

// Detail is a course with its chapters and lessons.
type Detail struct {
	Summary
	Intro       string          `json:"intro"`
	AuthorIntro string          `json:"authorIntro"`
	Chapters    []ChapterDetail `json:"chapters"`
	Progress    Progress        `json:"progress"`
	// RejectReason explains the last rejected publish request, while the
	// course is rejected.
	RejectReason string `json:"rejectReason,omitempty"`
	// Contributors are the users, other than the uploader, whose update
	// requests were approved.
	Contributors []string `json:"contributors"`
}

// Progress is the viewer's progress through a course.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
	// LastLessonID is the lesson the viewer opened last, if any.
	LastLessonID *int64 `json:"lastLessonId"`
}

type ChapterDetail struct {
	ID      int64          `json:"id"`
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Done    int            `json:"done"`
	Lessons []LessonDetail `json:"lessons"`
}

type LessonDetail struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Video    string `json:"video"` // "file", "url" or ""
	Duration string `json:"duration"`
	HasText  bool   `json:"hasText"`
	State    string `json:"state"`
	// Unavailable marks a lesson the viewer did not retain of a course that
	// left the library: it shows in the contents but cannot be opened.
	Unavailable bool `json:"unavailable,omitempty"`
}

// summaryColumns selects a Summary from courses c for the viewer bound to
// the query parameter viewerParam (e.g. "$2").
func summaryColumns(viewerParam string) string {
	return `
	c.id, c.title, c.type, c.completion, c.author_name, c.cover IS NOT NULL, c.visibility, c.publish_state,
	c.uploader_id, (SELECT count(*) FROM lessons l WHERE l.course_id = c.id),
	(SELECT count(*) FROM lesson_progress p JOIN lessons l ON l.course_id = p.course_id AND l.key = p.lesson_key
	 WHERE p.course_id = c.id AND p.user_id = ` + viewerParam + ` AND p.done),
	c.created_at, c.updated_at, c.files_dir,
	EXISTS (SELECT 1 FROM course_readers r WHERE r.course_id = c.id AND r.user_id = ` + viewerParam + ` AND r.subscribed_at IS NOT NULL),
	c.deleted_at IS NOT NULL`
}

// summaryFields are the scan targets matching summaryColumns.
func summaryFields(s *Summary) []any {
	return []any{&s.ID, &s.Title, &s.Type, &s.Completion, &s.AuthorName, &s.HasCover, &s.Visibility, &s.PublishState,
		&s.UploaderID, &s.LessonCount, &s.DoneCount, &s.CreatedAt, &s.UpdatedAt, &s.FilesDir, &s.Subscribed, &s.Deleted}
}

func scanSummary(row pgx.Row, viewer auth.User) (Summary, error) {
	var s Summary
	err := row.Scan(summaryFields(&s)...)
	s.IsUploader = err == nil && s.UploaderID == viewer.ID
	return s, err
}

// loadVisible loads one course the viewer may see, scanning the summary
// plus extraColumns into extra. Courses the viewer may not see are
// ErrNotFound, so private courses do not reveal that they exist.
func (s *Service) loadVisible(ctx context.Context, viewer auth.User, id int64, extraColumns string, extra ...any) (Summary, error) {
	sm, err := s.load(ctx, viewer, id, extraColumns, extra...)
	if err == nil && !canView(viewer, sm) {
		return Summary{}, ErrNotFound
	}
	return sm, err
}

// loadToChange loads a course for an operation that changes it: admins
// moderate every course, anyone else only reaches courses they can see.
func (s *Service) loadToChange(ctx context.Context, viewer auth.User, id int64) (Summary, error) {
	if viewer.IsAdmin() {
		return s.load(ctx, viewer, id, "")
	}
	return s.loadVisible(ctx, viewer, id, "")
}

// load loads one course whoever may see it; admins moderate through it. A
// deleted course is ErrNotFound.
func (s *Service) load(ctx context.Context, viewer auth.User, id int64, extraColumns string, extra ...any) (Summary, error) {
	sm, err := s.loadAny(ctx, viewer, id, extraColumns, extra...)
	if err == nil && sm.Deleted {
		return Summary{}, ErrNotFound
	}
	return sm, err
}

// loadAny loads one course, deleted or not.
func (s *Service) loadAny(ctx context.Context, viewer auth.User, id int64, extraColumns string, extra ...any) (Summary, error) {
	var sm Summary
	row := s.db.QueryRow(ctx, "SELECT "+summaryColumns("$2")+extraColumns+" FROM courses c WHERE c.id = $1", id, viewer.ID)
	err := row.Scan(append(summaryFields(&sm), extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, err
	}
	sm.IsUploader = sm.UploaderID == viewer.ID
	return sm, nil
}

// canView is the single rule for who may see a course: anyone signed in sees
// public courses; a private course is seen by its uploader, and by admins
// while it is under review. Nobody sees a deleted course as it is.
func canView(viewer auth.User, s Summary) bool {
	return !s.Deleted && (s.Visibility == Public || s.UploaderID == viewer.ID || (viewer.IsAdmin() && s.PublishState == Pending))
}

// Get returns a course with its chapters and lessons.
func (s *Service) Get(ctx context.Context, viewer auth.User, id int64) (Detail, error) {
	var d Detail
	sm, ret, err := s.open(ctx, viewer, id, ", c.intro, c.author_intro", &d.Intro, &d.AuthorIntro)
	if err != nil {
		return Detail{}, err
	}
	d.Summary = sm
	d.Progress = Progress{Done: sm.DoneCount, Total: sm.LessonCount}
	if ret != nil {
		return s.getRetained(ctx, viewer, d, ret)
	}
	if d.Chapters, err = s.structure(ctx, viewer, id); err != nil {
		return Detail{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT u.username FROM course_contributors cc JOIN users u ON u.id = cc.user_id
		WHERE cc.course_id = $1 ORDER BY cc.created_at`, id)
	if err != nil {
		return Detail{}, err
	}
	if d.Contributors, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return Detail{}, err
	}
	if sm.PublishState == Rejected && sm.Visibility == Private {
		if err := s.db.QueryRow(ctx, `
			SELECT reason FROM review_requests WHERE course_id = $1 AND kind = 'publish' AND state = 'rejected'
			ORDER BY decided_at DESC LIMIT 1`, id).Scan(&d.RejectReason); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Detail{}, err
		}
	}
	err = s.db.QueryRow(ctx, `
		SELECT l.id FROM course_readers r JOIN lessons l ON l.course_id = r.course_id AND l.key = r.last_lesson_key
		WHERE r.user_id = $1 AND r.course_id = $2`, viewer.ID, id).Scan(&d.Progress.LastLessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return d, err
}

// getRetained fills in a course the viewer reads from its freeze.
func (s *Service) getRetained(ctx context.Context, viewer auth.User, d Detail, ret *retention) (Detail, error) {
	d.Intro, d.AuthorIntro = ret.snap.Intro, ret.snap.AuthorIntro
	d.Chapters = ret.structure()
	d.Contributors = []string{}
	var key string
	err := s.db.QueryRow(ctx, "SELECT coalesce(last_lesson_key, '') FROM course_readers WHERE user_id = $1 AND course_id = $2",
		viewer.ID, d.ID).Scan(&key)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, err
	}
	if l, _, ok := ret.find(func(l snapLesson) bool { return l.Key == key }); ok {
		d.Progress.LastLessonID = &l.ID
	}
	return d, nil
}

func (s *Service) structure(ctx context.Context, viewer auth.User, courseID int64) ([]ChapterDetail, error) {
	rows, err := s.db.Query(ctx, `
		SELECT ch.id, ch.key, ch.title, l.id, l.key, l.title,
		       CASE WHEN l.video_file IS NOT NULL THEN 'file' WHEN l.video_url IS NOT NULL THEN 'url' ELSE '' END,
		       l.duration, l.file IS NOT NULL, `+lessonState+`
		FROM chapters ch JOIN lessons l ON l.chapter_id = ch.id
		LEFT JOIN lesson_progress p ON p.user_id = $2 AND p.course_id = l.course_id AND p.lesson_key = l.key
		WHERE ch.course_id = $1
		ORDER BY ch.position, l.position`, courseID, viewer.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChapterDetail
	for rows.Next() {
		var ch ChapterDetail
		var l LessonDetail
		if err := rows.Scan(&ch.ID, &ch.Key, &ch.Title, &l.ID, &l.Key, &l.Title, &l.Video, &l.Duration, &l.HasText, &l.State); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != ch.ID {
			out = append(out, ch)
		}
		last := &out[len(out)-1]
		last.Lessons = append(last.Lessons, l)
		if l.State == StateDone {
			last.Done++
		}
	}
	return out, rows.Err()
}

// MineFilter narrows my courses; empty fields match everything.
type MineFilter struct {
	// State is "unfinished" (not every lesson done yet) or "done".
	State      string
	Type       string
	Completion string
}

// Mine lists my courses: the library courses the user subscribes to, the
// courses that left the library of which they retain lessons, and every
// private course they uploaded, most recently studied first.
func (s *Service) Mine(ctx context.Context, viewer auth.User, f MineFilter) ([]Summary, error) {
	rows, err := s.db.Query(ctx, "SELECT "+summaryColumns("$1")+` FROM courses c
		LEFT JOIN course_readers r ON r.course_id = c.id AND r.user_id = $1
		WHERE (c.visibility = 'public' AND r.subscribed_at IS NOT NULL)
		   OR (c.visibility <> 'public' AND c.uploader_id = $1 AND c.deleted_at IS NULL)
		   OR (c.visibility <> 'public' AND r.subscribed_at IS NOT NULL
		       AND EXISTS (SELECT 1 FROM retained_lessons rl WHERE rl.course_id = c.id AND rl.user_id = $1))
		ORDER BY coalesce(r.last_studied_at, c.created_at) DESC, c.id DESC`, viewer.ID)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) { return scanSummary(r, viewer) })
	if err != nil {
		return nil, err
	}
	readsFrozen := func(c Summary) bool { return c.Visibility != Public && (!c.IsUploader || c.Deleted) }
	var frozen []int64
	for _, c := range all {
		if readsFrozen(c) {
			frozen = append(frozen, c.ID)
		}
	}
	rets, err := s.retentions(ctx, viewer, frozen)
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, c := range all {
		if readsFrozen(c) {
			ret := rets[c.ID]
			if ret == nil {
				continue
			}
			ret.apply(&c) // filters below see the frozen course
		}
		finished := c.LessonCount > 0 && c.DoneCount == c.LessonCount
		if (f.Type != "" && c.Type != f.Type) || (f.Completion != "" && c.Completion != f.Completion) ||
			(f.State != "" && (f.State == "done") != finished) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// ErrNoCover means the course has no cover image of its own.
var ErrNoCover = errors.New("course has no cover")

// summary loads one course's card data with its cover path ("" if none).
func (s *Service) summary(ctx context.Context, viewer auth.User, id int64) (Summary, string, error) {
	var cover *string
	sm, ret, err := s.open(ctx, viewer, id, ", c.cover", &cover)
	switch {
	case err != nil:
		return sm, "", err
	case ret != nil:
		return sm, ret.snap.Cover, nil
	case cover == nil:
		return sm, "", nil
	}
	return sm, *cover, nil
}

// Cover streams the course's cover image; name is its package path.
func (s *Service) Cover(ctx context.Context, viewer auth.User, id int64) (rc io.ReadCloser, name string, err error) {
	sm, cover, err := s.summary(ctx, viewer, id)
	if err != nil {
		return nil, "", err
	}
	if cover == "" {
		return nil, "", ErrNoCover
	}
	rc, err = s.files.Get(ctx, filesPrefix(id, sm.FilesDir)+cover)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, "", ErrNoCover
	}
	return rc, cover, err
}

// Delete removes a course. The uploader may delete a private course; admins
// may delete any course. While subscribers retain lessons of it (ADR-0004)
// the course is only marked deleted: its files and structure are frozen for
// them, and it is removed once the last of them lets go. Otherwise it goes
// at once, with its files.
func (s *Service) Delete(ctx context.Context, viewer auth.User, id int64) error {
	sm, err := s.loadToChange(ctx, viewer, id)
	if err != nil {
		return err
	}
	if !viewer.IsAdmin() && !(sm.IsUploader && sm.Visibility == Private) {
		return ErrForbidden
	}
	const reason = "课程已删除"
	var kept bool
	var gone leaving
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if kept, err = freeze(ctx, tx, id, RetentionDeleted); err != nil {
			return err
		}
		if gone, err = settleLeaving(ctx, tx, id, "", reason, viewer.ID); err != nil {
			return err
		}
		subs, err := subscribers(ctx, tx, id)
		if err != nil {
			return err
		}
		withRetained := func(p notify.CoursePayload, user int64) (any, error) {
			d := notify.DeletedPayload{CoursePayload: p}
			err := tx.QueryRow(ctx, "SELECT count(*) FROM retained_lessons WHERE course_id = $1 AND user_id = $2", id, user).Scan(&d.RetainedCount)
			return d, err
		}
		if err := notifyLeaving(ctx, tx, gone, notify.CourseDeleted, subs, withRetained); err != nil {
			return err
		}
		if !kept {
			return purge(ctx, tx, id)
		}
		// Only the freeze is read from now on, so the course keeps just its files.
		_, err = tx.Exec(ctx, `
			UPDATE courses SET visibility = 'private', publish_state = 'unpublished', deleted_at = now(), updated_at = now(),
				files_dir = (SELECT files_dir FROM course_freezes WHERE course_id = $1)
			WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if !kept {
		s.dropCourseFiles(ctx, id)
		return nil
	}
	s.dropUnused(ctx, id, sm.FilesDir) // newer than the freeze, if updated since unpublishing
	for _, r := range gone.settled {
		if r.filesDir != "" {
			s.dropFiles(ctx, id, r.filesDir)
		}
	}
	return nil
}

// purge removes a course for good; its files go with dropCourseFiles once
// the transaction commits.
func purge(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, "DELETE FROM courses WHERE id = $1", id)
	return err
}

// dropCourseFiles removes every file of a course that is gone. Files left
// behind by a failure here are unreachable and only cost space.
func (s *Service) dropCourseFiles(ctx context.Context, id int64) {
	if err := s.files.DeletePrefix(context.WithoutCancel(ctx), coursePrefix(id)); err != nil {
		slog.Warn("course files not removed", "course", id, "err", err)
	}
}
