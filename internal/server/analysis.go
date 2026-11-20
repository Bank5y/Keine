package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// analysisRequest is a selection to analyze, as a selection gives it.
type analysisRequest struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
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
	ctx := r.Context()
	turn, err := s.models.ForTurn(ctx, assistant.TurnAnalysis)
	if err != nil {
		if !errors.Is(err, assistant.ErrTurnUnmapped) {
			slog.Warn("analysis model unusable", "err", err)
		}
		writeError(w, http.StatusConflict, "ai_unavailable", "探索助手暂不可用")
		return
	}
	p, err := s.courses.Passage(ctx, currentUser(r), id, lessonID, anchor.Quote{Exact: in.Quote, Prefix: in.Prefix, Suffix: in.Suffix})
	if !writeAnnotationError(w, r, err) {
		return
	}

	sse, ok := startEvents(w)
	if !ok {
		writeInternal(w, r, errors.New("response cannot stream"))
		return
	}
	a, err := assistant.Analyze(ctx, turn, assistant.AnalysisPrompt.Messages(passage(p, in.Quote)), func(d assistant.Delta) error {
		return sse.send("delta", d)
	})
	var re *assistant.RunError
	switch {
	case err == nil:
		_ = sse.send("done", a)
	case errors.As(err, &re):
		slog.Info("analysis failed", "code", re.Code, "err", re.Cause)
		_ = sse.send("error", map[string]any{"code": re.Code, "kept": re.Kept})
	case ctx.Err() == nil:
		slog.Error("analysis failed", "err", err)
		_ = sse.send("error", map[string]any{"code": assistant.CodeProviderError, "kept": false})
	}
}

// passage is a course passage as the assistant reads it.
func passage(p course.Passage, quote string) assistant.Passage {
	out := assistant.Passage{
		CourseTitle: p.Course.Title,
		CourseIntro: p.Course.Intro,
		AuthorName:  p.Course.AuthorName,
		AuthorIntro: p.Course.AuthorIntro,
		Path:        append([]string{p.Chapter, p.Lesson.Title}, p.Headings...),
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
			oc.Lessons = append(oc.Lessons, title)
		}
		out.Outline = append(out.Outline, oc)
	}
	return out
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
