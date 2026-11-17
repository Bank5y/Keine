package course

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// The operations a course draft records (ADR-0006).
const (
	OpAddChapter        = "addChapter"
	OpRenameChapter     = "renameChapter"
	OpDeleteChapter     = "deleteChapter"
	OpMoveChapter       = "moveChapter"
	OpAddUpcomingLesson = "addUpcomingLesson"
	OpRenameLesson      = "renameLesson"
	OpDeleteLesson      = "deleteLesson"
	OpMoveLesson        = "moveLesson"
	OpSetInfo           = "setInfo"
	// Operations that give lessons content uploaded to the draft.
	OpAddLesson           = "addLesson"
	OpReplaceLesson       = "replaceLesson"
	OpUploadLessonContent = "uploadLessonContent"
)

// Op is one operation of a course draft, as stored and as sent by the
// editor. Type names the operation; the other fields are the ones it uses:
//
//	addChapter        {chapter, title}          chapter: the new key, set by the system; appended last
//	renameChapter     {chapter, title}
//	deleteChapter     {chapter}                 with all its lessons; never the last chapter
//	moveChapter       {chapter, after}          after: the chapter it follows, "" for first
//	addUpcomingLesson {lesson, chapter, title}  lesson: the new key, set by the system; appended last
//	renameLesson      {lesson, title}
//	deleteLesson      {lesson}
//	moveLesson        {lesson, chapter, after}  after: the lesson of chapter it follows, "" for first
//	setInfo           {info}                    only the fields given change
//	addLesson         {lesson, chapter, title, content}  lesson: the new key, set by the system; appended last
//	replaceLesson     {lesson, content, title?}  a lesson with content gets new content (and title)
//	uploadLessonContent {lesson, content}        an upcoming lesson gets its content
//
// Chapters and lessons are named by key, so an operation still finds its
// target once others have moved things around.
type Op struct {
	// ID tells the operation apart from every other, set by the system;
	// a conflict is confirmed by it. Operations recorded before IDs existed
	// are given "i{index}" as they are read.
	ID      string      `json:"id,omitempty"`
	Type    string      `json:"type"`
	Chapter string      `json:"chapter,omitempty"`
	Lesson  string      `json:"lesson,omitempty"`
	Title   string      `json:"title,omitempty"`
	After   *string     `json:"after,omitempty"`
	Info    *InfoChange `json:"info,omitempty"`
	// Content is the lesson content of addLesson, replaceLesson and
	// uploadLessonContent, from an upload of the draft.
	Content *LessonContent `json:"content,omitempty"`
	// Target is the title, when the operation was recorded, of the chapter
	// or lesson it names (the chapter for addLesson/addUpcomingLesson), so
	// a conflict can say what it was about once that is gone. Set by the
	// system.
	Target string `json:"target,omitempty"`
}

// Conflict is an operation of a draft that no longer applies to the
// course's latest version, for example because someone else's update
// deleted the lesson it renames (ADR-0006). ID is the operation's, by which
// the user confirms skipping it; Index is its place among the draft's
// operations, to show; Message tells the user why.
type Conflict struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Op      Op     `json:"op"`
	Message string `json:"message"`
}

// InfoChange changes a course's information; nil fields stay as they are.
// The author is the lecturer (讲师).
type InfoChange struct {
	Title       *string `json:"title,omitempty"`
	Completion  *string `json:"completion,omitempty"`
	Intro       *string `json:"intro,omitempty"`
	AuthorName  *string `json:"authorName,omitempty"`
	AuthorIntro *string `json:"authorIntro,omitempty"`
}

// OpRejected is an operation that cannot apply to the course as the draft
// has it; Message tells the user why, and Problems lists the details
// when there are several (such as each missing image).
type OpRejected struct {
	Message  string
	Problems []Problem
}

func (e *OpRejected) Error() string { return "operation rejected: " + e.Message }

func reject(format string, args ...any) error {
	return &OpRejected{Message: fmt.Sprintf(format, args...)}
}

// prepareOp turns an operation sent by the editor into the one the draft
// records: titles trimmed, and new chapters and lessons given keys of
// their own, which users never see or choose.
func prepareOp(op Op) (Op, error) {
	op.Title = strings.TrimSpace(op.Title)
	op.Target = ""
	op.ID = newKey("o")
	switch op.Type {
	case OpAddChapter:
		op.Chapter = newKey("c")
	case OpAddUpcomingLesson, OpAddLesson:
		op.Lesson = newKey("l")
	case OpSetInfo:
		if op.Info == nil {
			return op, reject("没有要修改的课程信息")
		}
		for _, f := range []*string{op.Info.Title, op.Info.Completion, op.Info.Intro, op.Info.AuthorName, op.Info.AuthorIntro} {
			if f != nil {
				*f = strings.TrimSpace(*f)
			}
		}
	}
	return op, nil
}

func newKey(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// replayOps applies a draft's operations in order to a course's manifest
// and returns the course the draft makes of it; m is left as it was. An
// operation that cannot apply is skipped and listed as a conflict, so a
// draft stays usable after the course has changed under it (ADR-0006).
func replayOps(m Manifest, ops []Op) (Manifest, []Conflict) {
	m = m.clone()
	var conflicts []Conflict
	for i, op := range ops {
		if err := m.apply(op); err != nil {
			conflicts = append(conflicts, Conflict{ID: op.ID, Index: i, Op: op, Message: rejectMessage(err)})
		}
	}
	return m, conflicts
}

func rejectMessage(err error) string {
	var r *OpRejected
	if errors.As(err, &r) {
		return r.Message
	}
	return "这一步无法应用到课程的最新版本"
}

// unconfirmed lists the conflicts whose operation is not in skip, which
// names operations by ID: a conflict the user has not seen is never
// skipped, though another took its place among the operations.
func unconfirmed(conflicts []Conflict, skip []string) []Conflict {
	var out []Conflict
	for _, c := range conflicts {
		if !slices.Contains(skip, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

// applied are the operations that are not conflicts.
func applied(ops []Op, conflicts []Conflict) []Op {
	out := make([]Op, 0, len(ops))
	for i, op := range ops {
		if !slices.ContainsFunc(conflicts, func(c Conflict) bool { return c.Index == i }) {
			out = append(out, op)
		}
	}
	return out
}

// target is the title of the chapter or lesson op names in m, if any.
func (m *Manifest) target(op Op) string {
	switch op.Type {
	case OpRenameChapter, OpDeleteChapter, OpMoveChapter, OpAddUpcomingLesson, OpAddLesson:
		if i := m.chapter(op.Chapter); i >= 0 {
			return m.Chapters[i].Title
		}
	case OpRenameLesson, OpDeleteLesson, OpMoveLesson, OpReplaceLesson, OpUploadLessonContent:
		if ci, li, ok := m.lesson(op.Lesson); ok {
			return m.Chapters[ci].Lessons[li].Title
		}
	}
	return ""
}

func (m Manifest) clone() Manifest {
	m.Chapters = slices.Clone(m.Chapters)
	for i := range m.Chapters {
		m.Chapters[i].Lessons = slices.Clone(m.Chapters[i].Lessons)
	}
	return m
}

// apply applies one operation, or leaves m as it was and says why not.
func (m *Manifest) apply(op Op) error {
	switch op.Type {
	case OpAddChapter:
		if err := checkTitle(op.Title, "章"); err != nil {
			return err
		}
		if op.Chapter == "" || m.chapter(op.Chapter) >= 0 {
			return reject("新章的标识无效")
		}
		m.Chapters = append(m.Chapters, Chapter{Key: op.Chapter, Title: op.Title})
	case OpRenameChapter:
		i, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if err := checkTitle(op.Title, "章"); err != nil {
			return err
		}
		m.Chapters[i].Title = op.Title
	case OpDeleteChapter:
		i, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if len(m.Chapters) == 1 {
			return reject("课程至少要有一章, 最后一章不能删除")
		}
		m.Chapters = slices.Delete(m.Chapters, i, i+1)
	case OpMoveChapter:
		i, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if op.After == nil || *op.After == op.Chapter || (*op.After != "" && m.chapter(*op.After) < 0) {
			return reject("要移到的位置不存在")
		}
		ch := m.Chapters[i]
		m.Chapters = slices.Delete(m.Chapters, i, i+1)
		at := 0
		if *op.After != "" {
			at = m.chapter(*op.After) + 1
		}
		m.Chapters = slices.Insert(m.Chapters, at, ch)
	case OpAddUpcomingLesson:
		i, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if err := checkTitle(op.Title, "讲"); err != nil {
			return err
		}
		if op.Lesson == "" {
			return reject("新讲的标识无效")
		}
		if _, _, ok := m.lesson(op.Lesson); ok {
			return reject("新讲的标识无效")
		}
		if m.Completion == Finished {
			return reject("已完结的课程不能有待更新的讲; 请先把完结状态改为连载中或缺失")
		}
		m.Chapters[i].Lessons = append(m.Chapters[i].Lessons, Lesson{Key: op.Lesson, Title: op.Title})
	case OpRenameLesson:
		ci, li, err := m.findLesson(op.Lesson)
		if err != nil {
			return err
		}
		if err := checkTitle(op.Title, "讲"); err != nil {
			return err
		}
		m.Chapters[ci].Lessons[li].Title = op.Title
	case OpDeleteLesson:
		ci, li, err := m.findLesson(op.Lesson)
		if err != nil {
			return err
		}
		m.Chapters[ci].Lessons = slices.Delete(m.Chapters[ci].Lessons, li, li+1)
	case OpMoveLesson:
		ci, li, err := m.findLesson(op.Lesson)
		if err != nil {
			return err
		}
		to, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if op.After == nil || *op.After == op.Lesson {
			return reject("要移到的位置不存在")
		}
		if *op.After != "" {
			if ac, _, ok := m.lesson(*op.After); !ok || ac != to {
				return reject("要移到的位置不存在")
			}
		}
		l := m.Chapters[ci].Lessons[li]
		m.Chapters[ci].Lessons = slices.Delete(m.Chapters[ci].Lessons, li, li+1)
		at := 0
		if *op.After != "" {
			_, at, _ = m.lesson(*op.After)
			at++
		}
		m.Chapters[to].Lessons = slices.Insert(m.Chapters[to].Lessons, at, l)
	case OpSetInfo:
		return m.setInfo(op.Info)
	case OpAddLesson:
		i, err := m.findChapter(op.Chapter)
		if err != nil {
			return err
		}
		if err := checkTitle(op.Title, "讲"); err != nil {
			return err
		}
		if _, _, ok := m.lesson(op.Lesson); op.Lesson == "" || ok {
			return reject("新讲的标识无效")
		}
		l, err := op.lessonContent(m.Type, Lesson{Key: op.Lesson, Title: op.Title})
		if err != nil {
			return err
		}
		m.Chapters[i].Lessons = append(m.Chapters[i].Lessons, l)
	case OpReplaceLesson, OpUploadLessonContent:
		ci, li, err := m.findLesson(op.Lesson)
		if err != nil {
			return err
		}
		l := m.Chapters[ci].Lessons[li]
		switch {
		case op.Type == OpReplaceLesson && l.Upcoming():
			return reject("这一讲还没有内容, 请用「上传内容」补全它")
		case op.Type == OpUploadLessonContent && !l.Upcoming():
			return reject("这一讲已经有内容了, 请用「替换」")
		}
		if op.Title != "" {
			if err := checkTitle(op.Title, "讲"); err != nil {
				return err
			}
			l.Title = op.Title
		}
		if l, err = op.lessonContent(m.Type, l); err != nil {
			return err
		}
		m.Chapters[ci].Lessons[li] = l
	default:
		return reject("不支持的操作 %q", op.Type)
	}
	return nil
}

func (op Op) lessonContent(typ string, l Lesson) (Lesson, error) {
	if op.Content == nil {
		return l, reject("没有这一讲的内容")
	}
	return op.Content.lesson(typ, l)
}

func (m *Manifest) setInfo(c *InfoChange) error {
	if c == nil {
		return reject("没有要修改的课程信息")
	}
	next := *m
	if c.Title != nil {
		if err := checkTitle(*c.Title, "课程"); err != nil {
			return err
		}
		next.Title = *c.Title
	}
	if c.Completion != nil {
		switch *c.Completion {
		case Finished, Ongoing, Incomplete:
		default:
			return reject("完结状态只能是已完结、连载中或缺失")
		}
		next.Completion = *c.Completion
	}
	if c.Intro != nil {
		next.Intro = *c.Intro
	}
	if c.AuthorName != nil {
		next.AuthorName = *c.AuthorName
	}
	if c.AuthorIntro != nil {
		next.AuthorIntro = *c.AuthorIntro
	}
	if next.Completion == Finished && next.hasUpcoming() {
		return reject("课程里还有待更新的讲, 不能设为已完结")
	}
	*m = next
	return nil
}

func checkTitle(title, what string) error {
	if title == "" {
		return reject("%s的标题不能为空", what)
	}
	if utf8.RuneCountInString(title) > maxTitleLength {
		return reject("%s的标题不能超过 %d 个字符", what, maxTitleLength)
	}
	return nil
}

// chapter is the index of the chapter with key k, or -1.
func (m *Manifest) chapter(k string) int {
	return slices.IndexFunc(m.Chapters, func(ch Chapter) bool { return ch.Key == k })
}

// lesson finds the lesson with key k.
func (m *Manifest) lesson(k string) (chapter, index int, ok bool) {
	for i, ch := range m.Chapters {
		if j := slices.IndexFunc(ch.Lessons, func(l Lesson) bool { return l.Key == k }); j >= 0 {
			return i, j, true
		}
	}
	return -1, -1, false
}

func (m *Manifest) findChapter(k string) (int, error) {
	if i := m.chapter(k); i >= 0 {
		return i, nil
	}
	return -1, reject("这一章已经不存在了")
}

func (m *Manifest) findLesson(k string) (int, int, error) {
	if ci, li, ok := m.lesson(k); ok {
		return ci, li, nil
	}
	return -1, -1, reject("这一讲已经不存在了")
}
