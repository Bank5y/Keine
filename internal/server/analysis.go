package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/credits"
	"github.com/Bank5y/keine/internal/lessonctx"
)

// analysisRequest is a selection to analyze, as a selection gives it.
type analysisRequest struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	// Length is the reader's choice of how much of a long lesson to send.
	Length assistant.Length `json:"length"`
}

// writeRunError answers a run turned away before it started (#131); it
// returns true for none.
func writeRunError(w http.ResponseWriter, r *http.Request, err error) bool {
	var length *assistant.LengthChoiceError
	switch {
	case err == nil:
		return true
	case errors.As(err, &length):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "length_choice_required", Message: "这一讲比较长，请选择要放入多少正文。", Length: length,
		}})
	case errors.Is(err, assistant.ErrContextTooLong):
		writeError(w, http.StatusUnprocessableEntity, "context_too_long", "这次要发送的内容超过了模型能读的长度，可以改选「只看相关小节」或缩短选区。")
	case errors.Is(err, assistant.ErrReportTooLong):
		writeError(w, http.StatusUnprocessableEntity, "report_too_long", "对话过长，暂时无法生成探索报告。")
	case errors.Is(err, assistant.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "busy", "现在使用的人比较多，请稍后再试。")
	case errors.Is(err, assistant.ErrRunInProgress):
		writeError(w, http.StatusConflict, "run_in_progress", "你有一个分析还在进行，结束后再试。")
	default:
		writeInternal(w, r, err)
	}
	return false
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
	turn, err := s.readerTurn(ctx, currentUser(r).ID, assistant.TurnAnalysis)
	if !writeOwnModelError(w, r, err) {
		return
	}
	p, err := s.courses.Passage(ctx, currentUser(r), id, lessonID, anchor.Quote{Exact: in.Quote, Prefix: in.Prefix, Suffix: in.Suffix})
	if !writeAnnotationError(w, r, err) {
		return
	}
	if !writeExplorationError(w, r, s.courses.CheckOverlap(ctx, currentUser(r), p)) {
		return
	}
	if !p.Marked {
		// the model still gets the quote, but not which of its places
		slog.Warn("selection not marked in the lesson source", "course", id, "lesson", lessonID)
	}

	msgs, narrow, err := lessonctx.AnalysisMessages(p, in.Quote, in.Length, assistant.LimitsOf(turn))
	if !writeRunError(w, r, err) {
		return
	}
	user := currentUser(r).ID
	if !turn.Own && !writeCreditsError(w, r, s.credits.Afford(ctx, user, credits.Estimate(assistant.EstimateMessages(msgs), turn))) {
		return
	}
	sse, leave, ok := s.streamRun(w, r, user)
	if !ok {
		return
	}
	defer leave()
	// the reader's analysis not pinned is gone once a new one starts
	s.staged.drop(user)

	run := assistant.Run{
		Turn:     turn,
		Tools:    append(assistant.CourseTools(lessonctx.NewLibrary(s.courses, currentUser(r), p.Course)), s.opts.ExtraTools...),
		Web:      turn.Web,
		Now:      s.opts.Now,
		Deadline: s.opts.RunDeadline,
	}
	a, meta, err := assistant.Analyze(ctx, run, msgs, assistant.Sink{
		Delta:    func(d assistant.Delta) error { return sse.send("delta", d) },
		Activity: func(a assistant.Activity) error { return sse.send("activity", a) },
	})
	s.recordRun(ctx, user, assistant.TurnAnalysis, turn, meta)
	if err != nil {
		_ = sse.send("error", failedRunEvent(ctx, turn, "analysis", err))
		return
	}
	lessonText := "full"
	if narrow {
		lessonText = "related"
	}
	snapshot := analysisSnapshot{Version: 1, Prompt: assistant.AnalysisPrompt.Version, LessonText: lessonText}
	for _, m := range msgs {
		snapshot.Messages = append(snapshot.Messages, snapshotMessage{Role: m.Role, Content: m.Content})
	}
	analysisID := s.staged.keep(user, stagedAnalysis{
		courseID: id, lessonID: lessonID, quote: anchor.Quote{Exact: in.Quote, Prefix: in.Prefix, Suffix: in.Suffix},
		analysis: a, snapshot: snapshot, until: s.opts.Now().Add(analysisTTL),
		meta: newTurnMeta(turn, assistant.AnalysisPrompt, meta),
	})
	_ = sse.send("done", analysisDone{AnalysisID: analysisID, Analysis: a, Degraded: meta.Degraded, Sources: meta.Sources})
}

// analysisDone is the done event of an analysis: the ID to pin it by
// (#133), the three parts, the capabilities that went off (the reader is
// told the run got no web results, which never goes into the document),
// and the turn's source list (ADR 0008), of which the page shows only
// whether there is any.
type analysisDone struct {
	AnalysisID string `json:"analysisId"`
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
	assistant.CodeOwnModelError:   "你的自带模型出错了，没有改用站点的模型。请检查自带模型的设置，或稍后再试。",
}

// runError is the error event of a run: its code, what it means, and
// whether what streamed before it stays.
func runError(code string, kept bool) map[string]any {
	return map[string]any{"code": code, "message": runMessages[code], "kept": kept}
}

// failedRunEvent is the error event of a run on turn that failed with
// err: runError, with the kind of failure when it was the reader's own
// model's.
func failedRunEvent(ctx context.Context, turn assistant.Turn, what string, err error) map[string]any {
	code, kept := runFailure(ctx, what, err)
	ev := runError(ownFailure(turn, code), kept)
	ownDetail(ev, turn, err)
	return ev
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
