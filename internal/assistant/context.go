package assistant

import (
	_ "embed"
	"fmt"
	"strings"
)

// Source is one kind of thing a turn puts in front of the model (#94).
// Each turn kind declares its sources and their order with its prompt; what
// a turn leaves out, the model looks up with tools.
type Source string

const (
	SourceCourseInfo Source = "course_info"
	SourceOutline    Source = "outline"
	// SourceSelection is the selected text, listed once with its heading
	// path.
	SourceSelection Source = "selection"
	// SourceLesson is the whole lesson text, the selected place marked.
	SourceLesson Source = "lesson"
)

// Prompt is a versioned skill prompt with the context sources it goes
// with; the version is what a saved turn records. System holds the skill's
// own prompt followed by the answer style.
type Prompt struct {
	Version string
	System  string
	Style   Style
	Sources []Source
}

// newPrompt is a skill's prompt answered in style: its own prompt, then
// the style's.
func newPrompt(version, skill string, style Style, sources ...Source) Prompt {
	return Prompt{Version: version, System: strings.TrimSpace(skill) + "\n\n" + style.Text, Style: style, Sources: sources}
}

// The headings each source begins with (Passage.section); a follow-up
// finds the sources it keeps by them.
const (
	headCourseInfo     = "## 课程信息"
	headOutline        = "## 课程目录"
	headSelection      = "## 选中的原文"
	headLesson         = "## 本讲全文"
	headLessonSections = "## 本讲小节目录"
)

//go:embed prompts/analysis.md
var analysisSystem string

// AnalysisPrompt is the prompt of an analysis. The sources that change
// least come first, so the start of the request can be cached.
var AnalysisPrompt = newPrompt("analysis-3", analysisSystem, DefaultStyle, SourceCourseInfo, SourceOutline, SourceSelection, SourceLesson)

// Selected marks the selected place in the lesson text.
const (
	SelectedOpen  = "<selected>"
	SelectedClose = "</selected>"
)

// Passage is a selection in a lesson and what surrounds it.
type Passage struct {
	CourseTitle string
	CourseIntro string
	AuthorName  string
	AuthorIntro string
	Outline     []OutlineChapter
	// Path is where the selection sits: chapter, lesson, then the headings
	// above it.
	Path []string
	// LessonKey identifies the lesson of the selection.
	LessonKey string
	Quote     string
	// Lesson is the lesson's Markdown with the selected place wrapped in
	// SelectedOpen and SelectedClose (left unmarked when it cannot be told
	// apart).
	Lesson string
	// Sections is the lesson's outline of sections, set when Lesson holds
	// only the sections around the selection (LengthRelated); each is
	// indented two spaces a level below the top.
	Sections []string
}

type OutlineChapter struct {
	Title   string
	Lessons []OutlineLesson
}

// OutlineLesson is a lesson of the outline: Key is the identifier the
// course tools take.
type OutlineLesson struct {
	Key   string
	Title string
}

// Messages puts the prompt and the passage's sources together as a
// request's messages.
func (p Prompt) Messages(in Passage) []Message {
	var parts []string
	for _, s := range p.Sources {
		parts = append(parts, in.section(s))
	}
	return []Message{
		{Role: RoleSystem, Content: p.System},
		{Role: RoleUser, Content: strings.Join(parts, "\n\n")},
	}
}

func (in Passage) section(s Source) string {
	var b strings.Builder
	switch s {
	case SourceCourseInfo:
		b.WriteString(headCourseInfo + "\n课程：" + in.CourseTitle)
		if in.CourseIntro != "" {
			b.WriteString("\n简介：" + in.CourseIntro)
		}
		if in.AuthorName != "" {
			b.WriteString("\n作者：" + in.AuthorName)
			if in.AuthorIntro != "" {
				b.WriteString("：" + in.AuthorIntro)
			}
		}
	case SourceOutline:
		b.WriteString(headOutline + "（方括号里是讲的标识）")
		for _, ch := range in.Outline {
			b.WriteString("\n- " + ch.Title)
			for _, l := range ch.Lessons {
				fmt.Fprintf(&b, "\n  - %s [%s]", l.Title, l.Key)
			}
		}
	case SourceSelection:
		fmt.Fprintf(&b, "%s\n位置：%s（本讲 [%s]）\n「%s」", headSelection, strings.Join(in.Path, " › "), in.LessonKey, in.Quote)
	case SourceLesson:
		if in.Sections == nil {
			fmt.Fprintf(&b, "%s（选中处用 %s 标出）\n%s", headLesson, SelectedOpen, strings.TrimSpace(in.Lesson))
			break
		}
		b.WriteString(headLessonSections)
		for _, s := range in.Sections {
			b.WriteString("\n" + s)
		}
		fmt.Fprintf(&b, "\n\n## 本讲相关小节（读者选了只看相关小节：选中处所在的小节和相邻小节，选中处用 %s 标出；其余小节可以用工具按需读取）\n%s", SelectedOpen, strings.TrimSpace(in.Lesson))
	}
	return b.String()
}
