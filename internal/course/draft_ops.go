package course

import (
	"crypto/rand"
	"encoding/hex"
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
//
// Chapters and lessons are named by key, so an operation still finds its
// target once others have moved things around.
type Op struct {
	Type    string      `json:"type"`
	Chapter string      `json:"chapter,omitempty"`
	Lesson  string      `json:"lesson,omitempty"`
	Title   string      `json:"title,omitempty"`
	After   *string     `json:"after,omitempty"`
	Info    *InfoChange `json:"info,omitempty"`
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
// has it; Message tells the user why.
type OpRejected struct{ Message string }

func (e *OpRejected) Error() string { return "operation rejected: " + e.Message }

func reject(format string, args ...any) error {
	return &OpRejected{fmt.Sprintf(format, args...)}
}

// prepareOp turns an operation sent by the editor into the one the draft
// records: titles trimmed, and new chapters and lessons given keys of
// their own, which users never see or choose.
func prepareOp(op Op) (Op, error) {
	op.Title = strings.TrimSpace(op.Title)
	switch op.Type {
	case OpAddChapter:
		op.Chapter = newKey("c")
	case OpAddUpcomingLesson:
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

// applyOps applies a draft's operations in order to a course's manifest and
// returns the course the draft makes of it; m is left as it was. The first
// operation that cannot apply stops it, as an *OpRejected.
func applyOps(m Manifest, ops []Op) (Manifest, error) {
	m = m.clone()
	for _, op := range ops {
		if err := m.apply(op); err != nil {
			return m, err
		}
	}
	return m, nil
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
	default:
		return reject("不支持的操作 %q", op.Type)
	}
	return nil
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
