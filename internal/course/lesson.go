package course

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/storage"
)

var (
	ErrLessonNotFound = errors.New("lesson not found")
	ErrFileNotFound   = errors.New("course file not found")
	// ErrLessonUpcoming means the lesson is an upcoming lesson: it has no
	// content to read or study yet.
	ErrLessonUpcoming = errors.New("lesson is upcoming")
)

// LessonText is one lesson as the reader shows it.
type LessonText struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	ChapterID int64  `json:"chapterId"`
	// Markdown is the lesson text, "" when the lesson has none.
	Markdown string `json:"markdown"`
	// File is the package path of the text; images in it are relative to
	// its folder.
	File string `json:"-"`
	// VideoFile (package path) or VideoURL (external link) is the
	// lesson's video, if it has one.
	VideoFile string `json:"-"`
	VideoURL  string `json:"-"`
	// State and Position are the viewer's progress in the lesson.
	State    string  `json:"state"`
	Position float64 `json:"position"`
}

// Lesson returns a lesson of a course the viewer may see.
func (s *Service) Lesson(ctx context.Context, viewer auth.User, courseID, lessonID int64) (LessonText, error) {
	sm, ret, err := s.open(ctx, viewer, courseID, "")
	if err != nil {
		return LessonText{}, err
	}
	return s.lesson(ctx, viewer, sm, ret, lessonID)
}

// lesson reads a lesson of a course already opened for the viewer.
func (s *Service) lesson(ctx context.Context, viewer auth.User, sm Summary, ret *retention, lessonID int64) (LessonText, error) {
	courseID := sm.ID
	if ret != nil {
		return s.retainedLesson(ctx, viewer, courseID, lessonID, ret)
	}
	var l LessonText
	var file *string
	err := s.db.QueryRow(ctx, `
		SELECT l.id, l.key, l.title, l.chapter_id, l.file, coalesce(l.video_file, ''), coalesce(l.video_url, ''),
		       `+lessonState+`, coalesce(p.position, 0)
		FROM lessons l
		LEFT JOIN lesson_progress p ON p.user_id = $3 AND p.course_id = l.course_id AND p.lesson_key = l.key
		WHERE l.id = $1 AND l.course_id = $2`, lessonID, courseID, viewer.ID).
		Scan(&l.ID, &l.Key, &l.Title, &l.ChapterID, &file, &l.VideoFile, &l.VideoURL, &l.State, &l.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return LessonText{}, ErrLessonNotFound
	}
	if err == nil && file == nil && l.VideoFile == "" && l.VideoURL == "" {
		return LessonText{}, ErrLessonUpcoming
	}
	if err != nil || file == nil {
		return l, err
	}
	l.File = *file
	return s.readText(ctx, courseID, sm.FilesDir, l)
}

// readText loads a lesson's Markdown from a folder of the course's files.
func (s *Service) readText(ctx context.Context, courseID int64, dir string, l LessonText) (LessonText, error) {
	rc, err := s.files.Get(ctx, filesPrefix(courseID, dir)+l.File)
	if err != nil {
		return LessonText{}, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxLessonSize))
	l.Markdown = string(b)
	return l, err
}

// retainedLesson returns a lesson the viewer retains, from the freeze.
func (s *Service) retainedLesson(ctx context.Context, viewer auth.User, courseID, lessonID int64, ret *retention) (LessonText, error) {
	sl, chapterID, ok := ret.retainedLesson(lessonID)
	if !ok {
		return LessonText{}, ErrLessonNotFound
	}
	l := LessonText{ID: sl.ID, Key: sl.Key, Title: sl.Title, ChapterID: chapterID, File: sl.File,
		VideoFile: sl.VideoFile, VideoURL: sl.VideoURL, State: StateUnstarted}
	if ret.done[sl.Key] {
		l.State = StateDone
	}
	err := s.db.QueryRow(ctx, "SELECT position FROM lesson_progress WHERE user_id = $1 AND course_id = $2 AND lesson_key = $3",
		viewer.ID, courseID, sl.Key).Scan(&l.Position)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LessonText{}, err
	}
	if l.File == "" {
		return l, nil
	}
	return s.readText(ctx, courseID, ret.filesDir, l)
}

// FileLink opens one stored file of a course the viewer may see, or, when
// a browser can fetch it from object storage or a CDN itself, gives that
// address instead (and opens nothing). Only the files the course
// references are stored, so nothing else can be reached.
func (s *Service) FileLink(ctx context.Context, viewer auth.User, courseID int64, path string) (io.ReadCloser, string, error) {
	sm, ret, err := s.open(ctx, viewer, courseID, "")
	if err != nil {
		return nil, "", err
	}
	if clean, ok := cleanPath(path); !ok || clean != path || (ret != nil && !ret.canRead(path)) {
		return nil, "", ErrFileNotFound
	}
	return s.linkOrOpen(ctx, filesPrefix(courseID, sm.FilesDir)+path)
}

// linker is a store that can hand out direct addresses (storage.Routed).
type linker interface {
	Link(ctx context.Context, key string) (string, error)
}

func (s *Service) linkOrOpen(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if l, ok := s.files.(linker); ok {
		// a bucket that does not answer the check is no reason to fail:
		// the file is still served from here
		u, err := l.Link(ctx, key)
		if err != nil {
			slog.Warn("no direct link to a file, serving it", "key", key, "err", err)
		} else if u != "" {
			return nil, u, nil
		}
	}
	rc, err := s.openFile(ctx, key)
	return rc, "", err
}

func (s *Service) openFile(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := s.files.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrInvalidKey) {
		return nil, ErrFileNotFound
	}
	return rc, err
}
