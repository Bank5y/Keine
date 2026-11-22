// Package lessonctx is what the assistant reads of a course for a run
// (ADR 0008): an analysis's context assembled from the passage of its
// selection, and the course as the course tools read it. The server and
// keine-eval share it, so an evaluation runs on the context readers get.
package lessonctx

import (
	"context"
	"slices"
	"strings"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/course"
)

// AnalysisMessages assembles the request of an analysis of the passage's
// selection (#131): the whole lesson, or, past the soft limit and as the
// reader chose, the sections around the selection. related tells which.
// It fails with *assistant.LengthChoiceError when a choice is needed, and
// with assistant.ErrContextTooLong when what would go is past the model's
// window.
func AnalysisMessages(p course.Passage, quote string, choice assistant.Length, limits assistant.Limits) (msgs []assistant.Message, related bool, err error) {
	sent := passage(p, quote)
	narrow, err := assistant.ChooseLength(p.Lesson.Markdown, choice, limits)
	if err != nil {
		return nil, false, err
	}
	if narrow {
		sent = relatedSections(p, sent)
	}
	msgs = assistant.AnalysisPrompt.Messages(sent)
	if assistant.EstimateMessages(msgs) > limits.Input {
		return nil, false, assistant.ErrContextTooLong
	}
	return msgs, narrow, nil
}

// NewLibrary is the course c as viewer reads it.
func NewLibrary(courses *course.Service, viewer auth.User, c course.Detail) *Library {
	return &Library{courses: courses, viewer: viewer, course: c}
}

// passage is a course passage as the assistant reads it.
func passage(p course.Passage, quote string) assistant.Passage {
	out := assistant.Passage{
		CourseTitle: p.Course.Title,
		CourseIntro: p.Course.Intro,
		AuthorName:  p.Course.AuthorName,
		AuthorIntro: p.Course.AuthorIntro,
		Path:        append([]string{p.Chapter, p.Lesson.Title}, p.Headings...),
		LessonKey:   p.Lesson.Key,
		Quote:       quote,
		Lesson:      p.Lesson.Markdown,
	}
	if p.Marked {
		md := p.Lesson.Markdown
		out.Lesson = md[:p.Start] + assistant.SelectedOpen + md[p.Start:p.End] + assistant.SelectedClose + md[p.End:]
	}
	for _, ch := range p.Course.Chapters {
		oc := assistant.OutlineChapter{Title: ch.Title}
		for _, l := range ch.Lessons {
			title := l.Title
			if l.Upcoming {
				title += "（未上线）"
			}
			oc.Lessons = append(oc.Lessons, assistant.OutlineLesson{Key: l.Key, Title: title})
		}
		out.Outline = append(out.Outline, oc)
	}
	return out
}

// relatedSections narrows a passage to the section of the selection and the ones
// next to it at its level, with the lesson's outline of sections (#131).
// A selection above the first section takes the text there and the first
// section. A lesson without sections, or a selection that cannot be told
// apart in it, stays whole.
func relatedSections(p course.Passage, in assistant.Passage) assistant.Passage {
	md := p.Lesson.Markdown
	all := anchor.Sections(md)
	// the lesson's own title heads no section of it
	var sections []anchor.Section
	for _, s := range all {
		if strings.TrimSpace(s.Title) != strings.TrimSpace(p.Lesson.Title) {
			sections = append(sections, s)
		}
	}
	if len(sections) == 0 || !p.Marked {
		return in
	}
	at := -1
	for i, s := range sections {
		if s.Start <= p.Start && p.Start < s.End {
			at = i // the innermost comes last
		}
	}
	top := sections[0].Level
	for _, s := range sections {
		top = min(top, s.Level)
	}
	var from, to int
	if at < 0 {
		// above the first section: what comes before it, and the first
		from, to = 0, sections[0].End
	} else {
		var level []int
		for i, s := range sections {
			if s.Level == sections[at].Level {
				level = append(level, i)
			}
		}
		pos := slices.Index(level, at)
		from, to = sections[level[max(pos-1, 0)]].Start, sections[level[min(pos+1, len(level)-1)]].End
	}
	text := md[from:p.Start] + assistant.SelectedOpen + md[p.Start:p.End] + assistant.SelectedClose + md[p.End:to]
	outline := make([]string, len(sections))
	for i, s := range sections {
		outline[i] = strings.Repeat("  ", s.Level-top) + "- " + s.Title
	}
	in.Lesson, in.Sections = text, outline
	return in
}

// Library is a course as its reader sees it, for the course tools:
// the outline of the version they read, and each lesson read for them.
type Library struct {
	courses *course.Service
	viewer  auth.User
	course  course.Detail
}

func (c *Library) Lessons() []assistant.LessonRef {
	var out []assistant.LessonRef
	for _, ch := range c.course.Chapters {
		for _, l := range ch.Lessons {
			out = append(out, assistant.LessonRef{Key: l.Key, Title: l.Title, Chapter: ch.Title, HasText: l.HasText, Upcoming: l.Upcoming, Unavailable: l.Unavailable})
		}
	}
	return out
}

func (c *Library) Text(ctx context.Context, key string) (string, error) {
	id, ok := c.course.LessonID(key)
	if !ok {
		return "", course.ErrLessonNotFound
	}
	t, err := c.courses.Lesson(ctx, c.viewer, c.course.ID, id)
	return t.Markdown, err
}
