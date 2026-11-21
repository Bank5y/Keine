package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/course"
)

// analysisRequest is a selection to analyze, as a selection gives it.
type analysisRequest struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	// Length is the reader's choice of how much of a long lesson to send.
	Length assistant.Length `json:"length"`
}

// lessonLength is a lesson past the soft limit, as the reader chooses how
// much of it to send: the limits and the lesson in tokens, its length in
// characters, and whether the whole of it still fits.
type lessonLength struct {
	assistant.Limits
	Lesson int  `json:"lesson"`
	Chars  int  `json:"chars"`
	Full   bool `json:"full"`
}

// analyze runs an analysis of a selection (ADR 0008). Whatever stops it
// from starting is a JSON error; once it starts, the answer streams as
// Server-Sent Events: delta, then done or error. A reader who goes away
// cancels the run.
func (s *Server) analyze(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	var in analysisRequest
	if !readJSON(w, r, &in) {
		return
	}
	if !assistant.ValidLength(in.Length) {
		writeError(w, http.StatusBadRequest, "bad_request", "请求格式不正确")
		return
	}
	ctx := r.Context()
	turn, err := s.models.ForTurn(ctx, assistant.TurnAnalysis)
	if !writeModelError(w, r, err) {
		return
	}
	p, err := s.courses.Passage(ctx, currentUser(r), id, lessonID, anchor.Quote{Exact: in.Quote, Prefix: in.Prefix, Suffix: in.Suffix})
	if !writeAnnotationError(w, r, err) {
		return
	}
	if !p.Marked {
		// the model still gets the quote, but not which of its places
		slog.Warn("selection not marked in the lesson source", "course", id, "lesson", lessonID)
	}

	limits := assistant.LimitsOf(turn)
	in2 := passage(p, in.Quote)
	if size := assistant.EstimateTokens(p.Lesson.Markdown); size > limits.Soft {
		switch in.Length {
		case assistant.LengthUnset:
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
				Code: "length_choice_required", Message: "这一讲比较长，请选择要放入多少正文。",
				Length: lessonLength{Limits: limits, Lesson: size, Chars: utf8.RuneCountInString(p.Lesson.Markdown), Full: size <= limits.Hard},
			}})
			return
		case assistant.LengthFull:
			if size > limits.Hard {
				writeContextTooLong(w)
				return
			}
		case assistant.LengthRelated:
			in2 = related(p, in2)
		}
	}
	msgs := assistant.AnalysisPrompt.Messages(in2)
	if assistant.EstimateMessages(msgs) > limits.Input {
		writeContextTooLong(w)
		return
	}
	leave, err := s.runs.Enter(currentUser(r).ID)
	switch {
	case errors.Is(err, assistant.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "busy", "现在使用的人比较多，请稍后再试。")
		return
	case errors.Is(err, assistant.ErrRunInProgress):
		writeError(w, http.StatusConflict, "run_in_progress", "你有一个分析还在进行，结束后再试。")
		return
	}
	defer leave()

	sse, ok := startEvents(w)
	if !ok {
		writeInternal(w, r, errors.New("response cannot stream"))
		return
	}
	run := assistant.Run{
		Turn:     turn,
		Tools:    append(assistant.CourseTools(&courseLibrary{s.courses, currentUser(r), p.Course}), s.opts.ExtraTools...),
		Web:      turn.Web,
		Now:      s.opts.Now,
		Deadline: s.opts.RunDeadline,
	}
	a, meta, err := assistant.Analyze(ctx, run, msgs, assistant.Sink{
		Delta:    func(d assistant.Delta) error { return sse.send("delta", d) },
		Activity: func(a assistant.Activity) error { return sse.send("activity", a) },
	})
	var re *assistant.RunError
	switch {
	case err == nil:
		_ = sse.send("done", analysisDone{Analysis: a, Degraded: meta.Degraded, Sources: meta.Sources})
	case errors.As(err, &re):
		slog.Info("analysis failed", "code", re.Code, "err", re.Cause)
		_ = sse.send("error", runError(re.Code, re.Kept))
	case ctx.Err() == nil:
		slog.Error("analysis failed", "err", err)
		_ = sse.send("error", runError(assistant.CodeProviderError, false))
	}
}

// analysisDone is the done event of an analysis: the three parts, the
// capabilities that went off (the reader is told the run got no web
// results, which never goes into the document), and the turn's source
// list (ADR 0008). The sources are the turn's metadata, made by the
// server; the page shows none of them, and until the first turn is kept
// on the server (#133) the event is where the turn's metadata goes.
type analysisDone struct {
	assistant.Analysis
	Degraded []string              `json:"degraded,omitempty"`
	Sources  []assistant.WebSource `json:"sources"`
}

// runMessages say what ended a run, in the reader's words.
var runMessages = map[string]string{
	assistant.CodeProviderError:   "模型服务暂时无法连接，请稍后再试。",
	assistant.CodeRateLimited:     "模型服务正繁忙，请稍后再试。",
	assistant.CodeTimeout:         "模型太久没有回应，请稍后再试。",
	assistant.CodeMalformedOutput: "助手的回答格式不对，请重试。",
	assistant.CodeOutputTruncated: "回答太长被截断了，请重试。",
}

// runError is the error event of a run: its code, what it means, and
// whether what streamed before it stays.
func runError(code string, kept bool) map[string]any {
	return map[string]any{"code": code, "message": runMessages[code], "kept": kept}
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

func writeContextTooLong(w http.ResponseWriter) {
	writeError(w, http.StatusUnprocessableEntity, "context_too_long", "这次要发送的内容超过了模型能读的长度，可以改选「只看相关小节」或缩短选区。")
}

// related narrows a passage to the section of the selection and the ones
// next to it at its level, with the lesson's outline of sections (#131).
// Without a section around the selection, the passage stays whole.
func related(p course.Passage, in assistant.Passage) assistant.Passage {
	md := p.Lesson.Markdown
	all := anchor.Sections(md)
	// the lesson's own title heads no section of it
	var sections []anchor.Section
	for _, s := range all {
		if strings.TrimSpace(s.Title) != strings.TrimSpace(p.Lesson.Title) {
			sections = append(sections, s)
		}
	}
	at := -1
	if p.Marked {
		for i, s := range sections {
			if s.Start <= p.Start && p.Start < s.End {
				at = i // the innermost comes last
			}
		}
	}
	if at < 0 {
		return in
	}
	top := sections[0].Level
	for _, s := range sections {
		top = min(top, s.Level)
	}
	var level []int
	for i, s := range sections {
		if s.Level == sections[at].Level {
			level = append(level, i)
		}
	}
	pos := slices.Index(level, at)
	from, to := sections[level[max(pos-1, 0)]].Start, sections[level[min(pos+1, len(level)-1)]].End
	text := md[from:p.Start] + assistant.SelectedOpen + md[p.Start:p.End] + assistant.SelectedClose + md[p.End:to]
	outline := make([]string, len(sections))
	for i, s := range sections {
		outline[i] = strings.Repeat("  ", s.Level-top) + "- " + s.Title
	}
	in.Lesson, in.Sections = text, outline
	return in
}

// courseLibrary is a course as its reader sees it, for the course tools:
// the outline of the version they read, and each lesson read for them.
type courseLibrary struct {
	courses *course.Service
	viewer  auth.User
	course  course.Detail
}

func (c *courseLibrary) Lessons() []assistant.LessonRef {
	var out []assistant.LessonRef
	for _, ch := range c.course.Chapters {
		for _, l := range ch.Lessons {
			out = append(out, assistant.LessonRef{Key: l.Key, Title: l.Title, Chapter: ch.Title, HasText: l.HasText, Upcoming: l.Upcoming, Unavailable: l.Unavailable})
		}
	}
	return out
}

func (c *courseLibrary) Text(ctx context.Context, key string) (string, error) {
	for _, ch := range c.course.Chapters {
		for _, l := range ch.Lessons {
			if l.Key == key {
				t, err := c.courses.Lesson(ctx, c.viewer, c.course.ID, l.ID)
				return t.Markdown, err
			}
		}
	}
	return "", course.ErrLessonNotFound
}

// eventStream writes Server-Sent Events, each flushed as it is sent.
type eventStream struct {
	w http.ResponseWriter
	f http.Flusher
}

func startEvents(w http.ResponseWriter) (*eventStream, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &eventStream{w: w, f: f}, true
}

func (e *eventStream) send(name string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := e.w.Write([]byte("event: " + name + "\ndata: " + string(b) + "\n\n")); err != nil {
		return err
	}
	e.f.Flush()
	return nil
}
