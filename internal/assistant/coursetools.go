package assistant

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Bank5y/keine/internal/anchor"
)

// Library is a course as the reader sees it now: the version they read,
// retained lessons included. It is all the course tools can reach.
type Library interface {
	// Lessons are the course's lessons in outline order.
	Lessons() []LessonRef
	// Text is the Markdown of a lesson that has text.
	Text(ctx context.Context, key string) (string, error)
}

// LessonRef is a lesson of the outline. Key is its stable identifier,
// which the tools take and give back.
type LessonRef struct {
	Key      string
	Title    string
	Chapter  string
	HasText  bool
	Upcoming bool
	// Unavailable: the reader cannot open it (a lesson of a course that
	// left the library, which they did not keep).
	Unavailable bool
}

// Course tool limits (#118).
const (
	// MaxLessonRead is how much of a lesson read_lesson gives, in
	// characters.
	MaxLessonRead = 30_000
	maxHits       = 20
	maxLessonHits = 5
	// characters shown on either side of a search hit
	hitContext = 40
)

// Course tool names, which are also their activities' kinds.
const (
	ToolReadLesson   = "read_lesson"
	ToolListSections = "list_sections"
	ToolSearchCourse = "search_course"
)

type readLessonArgs struct {
	Lesson  string `json:"lesson" desc:"讲的标识，即课程目录里每讲后面方括号中的标识"`
	Section string `json:"section,omitempty" desc:"小节的标识（list_sections 给出的）；不填就读整讲"`
}

type listSectionsArgs struct {
	Lesson string `json:"lesson" desc:"讲的标识，即课程目录里每讲后面方括号中的标识"`
}

type searchCourseArgs struct {
	Query string `json:"query" desc:"要在本课程各讲正文里查找的关键词，不区分大小写"`
}

// CourseTools are the read-only tools over the reader's course:
// read_lesson, list_sections and search_course. Each lesson's text is read
// at most once a run.
func CourseTools(lib Library) []Tool {
	c := &courseTools{lib: lib, texts: map[string]string{}}
	return []Tool{
		NewTool(ToolReadLesson,
			"读本课程某一讲的正文，或其中一个小节。正文很长时先用 list_sections 看小节，再按小节读。",
			func(a readLessonArgs) string { return c.target(a.Lesson, a.Section) }, c.readLesson),
		NewTool(ToolListSections,
			"列出本课程某一讲的小节（正文里的标题）和它们的标识。",
			func(a listSectionsArgs) string { return c.target(a.Lesson, "") }, c.listSections),
		NewTool(ToolSearchCourse,
			"在本课程所有讲的正文里查找关键词，返回出现的位置（讲和小节）和附近的文字。",
			func(a searchCourseArgs) string { return strings.TrimSpace(a.Query) }, c.searchCourse),
	}
}

type courseTools struct {
	lib   Library
	mu    sync.Mutex
	texts map[string]string
}

func (c *courseTools) lesson(key string) (LessonRef, bool) {
	for _, l := range c.lib.Lessons() {
		if l.Key == key {
			return l, true
		}
	}
	return LessonRef{}, false
}

// target is the lesson's title and the section's, for the reader to see;
// what the model wrote when they are not found.
func (c *courseTools) target(key, section string) string {
	l, ok := c.lesson(key)
	if !ok {
		return strings.TrimSpace(key + " " + section)
	}
	if section == "" {
		return l.Title
	}
	c.mu.Lock()
	text, read := c.texts[key]
	c.mu.Unlock()
	if s, ok := sectionOf(text, section); read && ok {
		return l.Title + " › " + s.Title
	}
	return l.Title + " › " + section
}

// text is a lesson's text, or what to tell the model when it has none.
func (c *courseTools) text(ctx context.Context, key string) (LessonRef, string, error) {
	l, ok := c.lesson(key)
	if !ok {
		return l, "", Invalid(ErrNotFound, fmt.Sprintf("本课程没有标识为 %q 的讲。讲的标识见上下文里课程目录中每讲后面的方括号。", key))
	}
	if l.Upcoming {
		return l, "", Invalid(ErrNotFound, fmt.Sprintf("「%s」还没有上线，没有正文。", l.Title))
	}
	if l.Unavailable {
		return l, "", Invalid(ErrNotFound, fmt.Sprintf("「%s」读者现在看不到（课程已下架，读者没有保留这一讲），不能读。", l.Title))
	}
	if !l.HasText {
		return l, "", Invalid(ReasonUnsupportedContent, fmt.Sprintf("「%s」是视频，没有文字正文。", l.Title))
	}
	c.mu.Lock()
	text, read := c.texts[key]
	c.mu.Unlock()
	if read {
		return l, text, nil
	}
	text, err := c.lib.Text(ctx, key)
	if err != nil {
		return l, "", err
	}
	c.mu.Lock()
	c.texts[key] = text
	c.mu.Unlock()
	return l, text, nil
}

func (c *courseTools) readLesson(ctx context.Context, a readLessonArgs) (string, error) {
	l, text, err := c.text(ctx, a.Lesson)
	if err != nil {
		return "", err
	}
	head := fmt.Sprintf("讲 [%s]「%s」（%s）", l.Key, l.Title, l.Chapter)
	if a.Section != "" {
		s, ok := sectionOf(text, a.Section)
		if !ok {
			return "", Invalid(ErrNotFound, fmt.Sprintf("「%s」里没有标识为 %q 的小节。先用 list_sections 查看这一讲的小节和标识。", l.Title, a.Section))
		}
		head += fmt.Sprintf(" › 小节 [%s]「%s」", s.ID, s.Title)
		text = text[s.Start:s.End]
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxLessonRead {
		text = string([]rune(text)[:MaxLessonRead]) +
			fmt.Sprintf("\n\n[正文超过 %d 字，后面的部分没有给出。请用 list_sections 查看小节，再按小节读。]", MaxLessonRead)
	}
	return head + "\n\n" + text, nil
}

func sectionOf(text, id string) (anchor.Section, bool) {
	for _, s := range anchor.Sections(text) {
		if s.ID == id {
			return s, true
		}
	}
	return anchor.Section{}, false
}

func (c *courseTools) listSections(ctx context.Context, a listSectionsArgs) (string, error) {
	l, text, err := c.text(ctx, a.Lesson)
	if err != nil {
		return "", err
	}
	var sections []anchor.Section
	for _, s := range anchor.Sections(text) {
		// a heading that only repeats the lesson title is no section
		if strings.TrimSpace(s.Title) != strings.TrimSpace(l.Title) {
			sections = append(sections, s)
		}
	}
	if len(sections) == 0 {
		return fmt.Sprintf("讲 [%s]「%s」没有小节标题，可以用 read_lesson 读整讲。", l.Key, l.Title), nil
	}
	top := sections[0].Level
	for _, s := range sections {
		top = min(top, s.Level)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "讲 [%s]「%s」的小节（方括号里是标识）：", l.Key, l.Title)
	for _, s := range sections {
		fmt.Fprintf(&b, "\n%s- [%s] %s", strings.Repeat("  ", s.Level-top), s.ID, s.Title)
	}
	return b.String(), nil
}

func (c *courseTools) searchCourse(ctx context.Context, a searchCourseArgs) (string, error) {
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return "", Invalid(ErrInvalidRequest, "关键词不能为空。")
	}
	needle := lowerRunes(q)
	var hits []string
	total := 0
	for _, l := range c.lib.Lessons() {
		if !l.HasText || l.Upcoming || l.Unavailable {
			continue
		}
		_, text, err := c.text(ctx, l.Key)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			// one lesson that cannot be read leaves the rest searchable
			slog.Error("search_course: lesson unreadable", "lesson", l.Key, "err", err)
			continue
		}
		runes := []rune(text)
		// offsets[i] is where rune i starts in text, in bytes
		offsets := make([]int, len(runes)+1)
		for i, at := 0, 0; i < len(runes); i++ {
			offsets[i] = at
			at += utf8.RuneLen(runes[i])
		}
		offsets[len(runes)] = len(text)
		sections := anchor.Sections(text)
		n := 0
		for _, at := range indexAll(lowerRunes(text), needle) {
			total++
			if n >= maxLessonHits || len(hits) >= maxHits {
				continue
			}
			n++
			where := fmt.Sprintf("讲 [%s]「%s」", l.Key, l.Title)
			for _, s := range sections {
				if s.Start <= offsets[at] && offsets[at] < s.End {
					// the innermost section holding it comes last
					where = fmt.Sprintf("讲 [%s]「%s」› 小节 [%s]「%s」", l.Key, l.Title, s.ID, s.Title)
				}
			}
			hits = append(hits, "- "+where+"："+snippet(runes, at, at+len(needle)))
		}
	}
	if total == 0 {
		return fmt.Sprintf("本课程的正文里没有找到「%s」。", q), nil
	}
	head := fmt.Sprintf("「%s」在本课程正文里出现 %d 处", q, total)
	if len(hits) < total {
		head += fmt.Sprintf("，列出其中 %d 处", len(hits))
	}
	return head + "：\n" + strings.Join(hits, "\n"), nil
}

// lowerRunes is s in lower case, rune for rune, so positions carry over.
func lowerRunes(s string) []rune {
	r := []rune(s)
	for i := range r {
		r[i] = unicode.ToLower(r[i])
	}
	return r
}

// indexAll is where needle starts in r, without overlaps.
func indexAll(r, needle []rune) []int {
	var out []int
	for i := 0; i+len(needle) <= len(r); i++ {
		if slices.Equal(r[i:i+len(needle)], needle) {
			out = append(out, i)
			i += len(needle) - 1
		}
	}
	return out
}

// snippet is the text around runes [start, end), on one line.
func snippet(r []rune, start, end int) string {
	s := "…" + string(r[max(0, start-hitContext):min(len(r), end+hitContext)]) + "…"
	return strings.Join(strings.Fields(s), " ")
}
