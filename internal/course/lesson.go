package course

import (
	"context"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/storage"
)

var (
	ErrLessonNotFound = errors.New("lesson not found")
	ErrFileNotFound   = errors.New("course file not found")
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
	if _, err := s.loadVisible(ctx, viewer, courseID, ""); err != nil {
		return LessonText{}, err
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
	if err != nil || file == nil {
		return l, err
	}
	l.File = *file
	rc, err := s.files.Get(ctx, prefix(courseID)+l.File)
	if err != nil {
		return LessonText{}, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxLessonSize))
	l.Markdown = string(b)
	return l, err
}

// File streams one stored file of a course the viewer may see. Only the
// files the course references are stored, so nothing else can be reached.
func (s *Service) File(ctx context.Context, viewer auth.User, courseID int64, path string) (io.ReadCloser, error) {
	if _, err := s.loadVisible(ctx, viewer, courseID, ""); err != nil {
		return nil, err
	}
	if clean, ok := cleanPath(path); !ok || clean != path {
		return nil, ErrFileNotFound
	}
	rc, err := s.files.Get(ctx, prefix(courseID)+path)
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrInvalidKey) {
		return nil, ErrFileNotFound
	}
	return rc, err
}
