package course

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
)

// NoteCourse is a course on the notes page, with how many of the viewer's
// annotations on it match the filter.
type NoteCourse struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
}

// NoteLesson is the lesson an annotation is on.
type NoteLesson struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Chapter string `json:"chapter"`
}

// NoteEntry is one card of the notes page: an annotation with where it is.
type NoteEntry struct {
	Annotation
	Course struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"course"`
	// Lesson is nil when the course no longer has the lesson.
	Lesson *NoteLesson `json:"lesson"`
	// courseAt, lessonAt order the entry by its course and lesson; file and
	// dir find its lesson text.
	courseAt, lessonAt int
	file, dir          string
}

// Notebook is the notes page.
type Notebook struct {
	Annotations []NoteEntry  `json:"annotations"`
	Courses     []NoteCourse `json:"courses"`
}

// noteLesson is a lesson of a course as the viewer reads it.
type noteLesson struct {
	NoteLesson
	at   int
	file string
}

// readableLessons maps the keys of the lessons the viewer may read of an
// opened course to those lessons.
func (s *Service) readableLessons(ctx context.Context, sm Summary, ret *retention) (map[string]noteLesson, error) {
	out := map[string]noteLesson{}
	if ret != nil {
		at := 0
		for _, ch := range ret.snap.Chapters {
			for _, l := range ch.Lessons {
				if ret.retained[l.Key] {
					out[l.Key] = noteLesson{NoteLesson{l.ID, l.Title, ch.Title}, at, l.File}
				}
				at++
			}
		}
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.key, l.id, l.title, ch.title, coalesce(l.file, '')
		FROM chapters ch JOIN lessons l ON l.chapter_id = ch.id
		WHERE ch.course_id = $1
		ORDER BY ch.position, l.position`, sm.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for at := 0; rows.Next(); at++ {
		var key string
		var l noteLesson
		if err := rows.Scan(&key, &l.ID, &l.Title, &l.Chapter, &l.file); err != nil {
			return nil, err
		}
		l.at = at
		out[key] = l
	}
	return out, rows.Err()
}

// NoteFilter narrows the notes page; empty fields match everything.
type NoteFilter struct {
	CourseID int64
	// Style is underline or highlight; Color a highlight colour.
	Style Style
	Color string
	// Noted keeps only the annotations with a note.
	Noted bool
	// Query is text to find in the quote or the note, in any case.
	Query string
	// Sort is SortTime (the default) or SortText.
	Sort string
}

// The orders of the notes page: newest first, or as the text reads (course
// by course as listed, lesson by lesson, then by place in the lesson).
const (
	SortTime = "time"
	SortText = "text"
)

// matches tells whether an annotation passes the filter, whatever its course.
func (f NoteFilter) matches(a Annotation) bool {
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" &&
		!strings.Contains(strings.ToLower(a.Quote), q) && (a.Note == nil || !strings.Contains(strings.ToLower(*a.Note), q)) {
		return false
	}
	return (f.Style == "" || a.Style == f.Style) &&
		(f.Color == "" || (a.Color != nil && *a.Color == f.Color)) &&
		(!f.Noted || a.Note != nil)
}

func (f NoteFilter) valid() bool {
	return (f.Style == "" || f.Style == StyleUnderline || f.Style == StyleHighlight) &&
		(f.Color == "" || highlightColors[f.Color]) &&
		(f.Sort == "" || f.Sort == SortTime || f.Sort == SortText)
}

// byText orders entries as the text reads; an annotation the text no longer
// holds comes after those of its lesson it does.
func byText(x, y NoteEntry) int {
	if c := cmp.Compare(x.courseAt, y.courseAt); c != 0 {
		return c
	}
	if c := cmp.Compare(x.lessonAt, y.lessonAt); c != 0 {
		return c
	}
	switch {
	case x.Range != nil && y.Range != nil:
		if c := cmp.Compare(x.Range.Start, y.Range.Start); c != 0 {
			return c
		}
	case x.Range != nil:
		return -1
	case y.Range != nil:
		return 1
	}
	return x.CreatedAt.Compare(y.CreatedAt)
}

// Notebook lists the viewer's annotations on the courses they may still
// read, newest first, with those courses.
func (s *Service) Notebook(ctx context.Context, viewer auth.User, f NoteFilter) (Notebook, error) {
	if !f.valid() {
		return Notebook{}, ErrBadInput
	}
	rows, err := s.db.Query(ctx, "SELECT course_id, lesson_key, "+annotationColumns+`
		FROM annotations WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, viewer.ID)
	if err != nil {
		return Notebook{}, err
	}
	type row struct {
		courseID int64
		key      string
		a        Annotation
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := scanAnnotation(r, &x.a, &x.courseID, &x.key)
		return x, err
	})
	if err != nil {
		return Notebook{}, err
	}

	type opened struct {
		at      int
		sm      Summary
		lessons map[string]noteLesson
	}
	courses := map[int64]*opened{}
	nb := Notebook{Annotations: []NoteEntry{}, Courses: []NoteCourse{}}
	for _, x := range all {
		c, seen := courses[x.courseID]
		if !seen {
			sm, ret, err := s.open(ctx, viewer, x.courseID, "")
			if errors.Is(err, ErrNotFound) {
				courses[x.courseID] = nil
				continue
			}
			if err != nil {
				return Notebook{}, err
			}
			lessons, err := s.readableLessons(ctx, sm, ret)
			if err != nil {
				return Notebook{}, err
			}
			c = &opened{len(nb.Courses), sm, lessons}
			courses[x.courseID] = c
			nb.Courses = append(nb.Courses, NoteCourse{ID: sm.ID, Title: sm.Title})
		}
		if c == nil || !f.matches(x.a) {
			continue
		}
		e := NoteEntry{Annotation: x.a, courseAt: c.at, dir: c.sm.FilesDir}
		e.Course.ID, e.Course.Title = c.sm.ID, c.sm.Title
		if l, ok := c.lessons[x.key]; ok {
			e.Lesson, e.lessonAt, e.file = &l.NoteLesson, l.at, l.file
		} else if c.sm.Retention != "" {
			continue // a lesson the subscriber does not retain
		} else {
			e.lessonAt = len(c.lessons)
		}
		nb.Courses[c.at].Count++
		if f.CourseID == 0 || f.CourseID == c.sm.ID {
			nb.Annotations = append(nb.Annotations, e)
		}
	}
	if err := s.locateEntries(ctx, nb.Annotations); err != nil {
		return Notebook{}, err
	}
	if f.Sort == SortText {
		slices.SortStableFunc(nb.Annotations, byText)
	}
	return nb, nil
}

// locateEntries finds each entry in its lesson text, reading each lesson
// once.
func (s *Service) locateEntries(ctx context.Context, entries []NoteEntry) error {
	texts := map[[2]any]anchor.Text{}
	for i := range entries {
		e := &entries[i]
		if e.Lesson == nil || e.file == "" {
			continue
		}
		k := [2]any{e.Course.ID, e.Lesson.ID}
		text, ok := texts[k]
		if !ok {
			l, err := s.readText(ctx, e.Course.ID, e.dir, LessonText{File: e.file})
			if err != nil {
				return err
			}
			text = anchor.MarkdownText(l.Markdown)
			texts[k] = text
		}
		locate(text, &e.Annotation)
	}
	return nil
}
