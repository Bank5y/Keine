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
}

// Lesson returns a lesson of a course the viewer may see.
func (s *Service) Lesson(ctx context.Context, viewer auth.User, courseID, lessonID int64) (LessonText, error) {
	if _, err := s.loadVisible(ctx, viewer, courseID, ""); err != nil {
		return LessonText{}, err
	}
	var l LessonText
	var file *string
	err := s.db.QueryRow(ctx, "SELECT id, key, title, chapter_id, file FROM lessons WHERE id = $1 AND course_id = $2", lessonID, courseID).
		Scan(&l.ID, &l.Key, &l.Title, &l.ChapterID, &file)
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
