package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/assistant"
)

// streamRun starts a reader's run once its checks passed: it takes one of
// the reader's runs (ADR 0008) and opens the stream of its events. When ok,
// the caller defers leave.
func (s *Server) streamRun(w http.ResponseWriter, r *http.Request, userID int64) (sse *eventStream, leave func(), ok bool) {
	leave, err := s.runs.Enter(userID)
	if !writeRunError(w, r, err) {
		return nil, nil, false
	}
	sse, ok = startEvents(w)
	if !ok {
		leave()
		writeInternal(w, r, errors.New("response cannot stream"))
		return nil, nil, false
	}
	return sse, leave, true
}

// recordRun records what a run spent, failed or cut short too (ADR 0010),
// even when the reader went away. The site's own runs are user 0.
func (s *Server) recordRun(ctx context.Context, userID int64, kind assistant.TurnKind, turn assistant.Turn, meta assistant.Meta) {
	if err := s.credits.RecordRun(context.WithoutCancel(ctx), userID, kind, turn, meta); err != nil {
		slog.Error("usage not recorded", "user", userID, "kind", kind, "err", err)
	}
}

// runFailure is how a run that failed with err ends: its error code, and
// whether what streamed before stays. A RunError says so itself; any other
// error is the provider failing, logged unless the reader went away (ctx
// done). what names the run in the log.
func runFailure(ctx context.Context, what string, err error) (code string, kept bool) {
	var re *assistant.RunError
	if errors.As(err, &re) {
		slog.Info(what+" failed", "code", re.Code, "err", re.Cause)
		return re.Code, re.Kept
	}
	if ctx.Err() == nil {
		slog.Error(what+" failed", "err", err)
	}
	return assistant.CodeProviderError, false
}
