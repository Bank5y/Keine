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

// Import creates a private course from a course package and enrolls the
// uploader. Either the whole course is created or nothing is: an invalid
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
		if _, err := tx.Exec(ctx, "INSERT INTO enrollments (user_id, course_id) VALUES ($1, $2)", uploader.ID, id); err != nil {
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
	// Enrolled tells whether the viewer has started learning the course.
	Enrolled bool `json:"enrolled"`
	// DoneCount is how many lessons the viewer has finished.
	DoneCount int       `json:"doneCount"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// FilesDir is the folder of the course's files in use.
	FilesDir string `json:"-"`
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
	EXISTS (SELECT 1 FROM enrollments e WHERE e.course_id = c.id AND e.user_id = ` + viewerParam + `)`
}

// summaryFields are the scan targets matching summaryColumns.
func summaryFields(s *Summary) []any {
	return []any{&s.ID, &s.Title, &s.Type, &s.Completion, &s.AuthorName, &s.HasCover, &s.Visibility, &s.PublishState,
		&s.UploaderID, &s.LessonCount, &s.DoneCount, &s.CreatedAt, &s.UpdatedAt, &s.FilesDir, &s.Enrolled}
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

// load loads one course whoever may see it; admins moderate through it.
func (s *Service) load(ctx context.Context, viewer auth.User, id int64, extraColumns string, extra ...any) (Summary, error) {
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
// while it is under review.
func canView(viewer auth.User, s Summary) bool {
	return s.Visibility == Public || s.UploaderID == viewer.ID || (viewer.IsAdmin() && s.PublishState == Pending)
}

// Get returns a course with its chapters and lessons.
func (s *Service) Get(ctx context.Context, viewer auth.User, id int64) (Detail, error) {
	var d Detail
	sm, err := s.loadVisible(ctx, viewer, id, ", c.intro, c.author_intro", &d.Intro, &d.AuthorIntro)
	if err != nil {
		return Detail{}, err
	}
	d.Summary = sm
	if d.Chapters, err = s.structure(ctx, viewer, id); err != nil {
		return Detail{}, err
	}
	d.Progress = Progress{Done: sm.DoneCount, Total: sm.LessonCount}
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
		SELECT l.id FROM enrollments e JOIN lessons l ON l.course_id = e.course_id AND l.key = e.last_lesson_key
		WHERE e.user_id = $1 AND e.course_id = $2`, viewer.ID, id).Scan(&d.Progress.LastLessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return d, err
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

// Mine lists the courses a user has started learning, most recently studied
// first.
func (s *Service) Mine(ctx context.Context, viewer auth.User) ([]Summary, error) {
	rows, err := s.db.Query(ctx, "SELECT "+summaryColumns("$1")+` FROM courses c
		JOIN enrollments e ON e.course_id = c.id AND e.user_id = $1
		WHERE c.visibility = 'public' OR c.uploader_id = $1
		ORDER BY e.last_studied_at DESC, c.id DESC`, viewer.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) { return scanSummary(r, viewer) })
}

// ErrNoCover means the course has no cover image of its own.
var ErrNoCover = errors.New("course has no cover")

// summary loads one course's card data with its cover path ("" if none).
func (s *Service) summary(ctx context.Context, viewer auth.User, id int64) (Summary, string, error) {
	var cover *string
	sm, err := s.loadVisible(ctx, viewer, id, ", c.cover", &cover)
	if err != nil || cover == nil {
		return sm, "", err
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

// Delete removes a course and its files. The uploader may delete a private
// course; admins may delete any course.
func (s *Service) Delete(ctx context.Context, viewer auth.User, id int64) error {
	var sm Summary
	var err error
	if viewer.IsAdmin() {
		sm, err = s.load(ctx, viewer, id, "")
	} else {
		sm, _, err = s.summary(ctx, viewer, id)
	}
	if err != nil {
		return err
	}
	if !viewer.IsAdmin() && !(sm.IsUploader && sm.Visibility == Private) {
		return ErrForbidden
	}
	if _, err := s.db.Exec(ctx, "DELETE FROM courses WHERE id = $1", id); err != nil {
		return err
	}
	// The course is gone either way; files left behind by a failure here are
	// unreachable and only cost space.
	if err := s.files.DeletePrefix(ctx, coursePrefix(id)); err != nil {
		slog.Warn("course files not removed", "course", id, "err", err)
	}
	return nil
}
