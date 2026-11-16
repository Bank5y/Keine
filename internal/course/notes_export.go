package course

import (
	"context"
	"strings"

	"github.com/Bank5y/keine/internal/auth"
)

// NotesExport is a course's annotations written out as Markdown.
type NotesExport struct {
	Title    string
	Markdown string
}

// detachedState names why an entry is a detached annotation, or "" when the
// text still holds it.
//
// TODO(#37): once annotations carry their own state (待迁移, 原文已变化,
// 原文已删除), read it here instead of inferring it from the lesson and range.
func detachedState(e NoteEntry) string {
	switch {
	case e.Lesson == nil:
		return "原文已删除"
	case e.Range == nil:
		return "原文已变化"
	}
	return ""
}

// ExportNotes writes the viewer's annotations on a course as Markdown, in the
// manner of Kindle and Readwise exports: the course title, a section per
// lesson in course order, and each annotation as a quote followed by its
// note, as the text reads. Annotations on lessons the course no longer has
// come last, under their own heading.
func (s *Service) ExportNotes(ctx context.Context, viewer auth.User, courseID int64) (NotesExport, error) {
	nb, err := s.Notebook(ctx, viewer, NoteFilter{CourseID: courseID, Sort: SortText})
	if err != nil {
		return NotesExport{}, err
	}
	var out NotesExport
	for _, c := range nb.Courses {
		if c.ID == courseID {
			out.Title = c.Title
		}
	}
	if out.Title == "" {
		return NotesExport{}, ErrNotFound
	}

	var b strings.Builder
	b.WriteString("# " + oneLine(out.Title) + "\n")
	var section *NoteLesson
	for i, e := range nb.Annotations {
		if i == 0 || !sameLesson(section, e.Lesson) {
			section = e.Lesson
			if section == nil {
				b.WriteString("\n## 已删除的讲\n")
			} else {
				b.WriteString("\n## " + oneLine(section.Chapter) + " · " + oneLine(section.Title) + "\n")
			}
		}
		b.WriteString("\n" + quoteBlock(e.Quote) + "\n")
		if st := detachedState(e); st != "" {
			b.WriteString("\n*原文摘录：" + st + "*\n")
		}
		if e.Note != nil && strings.TrimSpace(*e.Note) != "" {
			b.WriteString("\n" + strings.TrimSpace(*e.Note) + "\n")
		}
	}
	out.Markdown = b.String()
	return out, nil
}

func sameLesson(a, b *NoteLesson) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// quoteBlock writes text as a Markdown block quote, line by line.
func quoteBlock(text string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	for i, l := range lines {
		if l = strings.TrimRight(l, " \t"); l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}
