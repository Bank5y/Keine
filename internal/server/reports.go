package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/credits"
)

// reportDeadlines is how many times a run's deadline a report has: it
// writes far more than a turn and looks nothing up.
const reportDeadlines = 3

func writeReportError(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, course.ErrReportNotFound) {
		writeError(w, http.StatusNotFound, "report_not_found", "这一版报告不存在。")
		return false
	}
	return writeExplorationError(w, r, err)
}

// generateReport writes a new version of an exploration's report (ADR
// 0012, #160): the report skill reads the whole dialogue tree. It is no
// turn: the dialogue gains nothing. Whatever stops it from starting (the
// tree too long for the model, the reader's credits) is a JSON error and
// costs nothing; once it starts, its parts stream as Server-Sent Events,
// and done carries the version kept.
func (s *Server) generateReport(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	user := currentUser(r)
	d, err := s.courses.ExplorationDetail(ctx, user, id)
	if !writeExplorationError(w, r, err) {
		return
	}
	turn, err := s.models.ForTurn(ctx, assistant.TurnReport)
	if !writeModelError(w, r, err) {
		return
	}
	msgs, taken, refs, err := s.reportMessages(ctx, user, d)
	if !writeExplorationError(w, r, err) {
		return
	}
	size := assistant.EstimateMessages(msgs)
	if size > assistant.ReportInput(turn) {
		writeError(w, http.StatusUnprocessableEntity, "report_too_long", "对话过长，暂时无法生成探索报告。")
		return
	}
	if !writeRunCreditsError(w, r, s.credits.Afford(ctx, user.ID, credits.Estimate(size, turn)), "生成报告") {
		return
	}
	leave, err := s.runs.Enter(user.ID)
	if !writeRunError(w, r, err) {
		return
	}
	defer leave()

	sse, ok := startEvents(w)
	if !ok {
		writeInternal(w, r, errors.New("response cannot stream"))
		return
	}
	deadline := s.opts.RunDeadline
	if deadline <= 0 {
		deadline = assistant.DefaultDeadline
	}
	run := assistant.Run{Turn: turn, Now: s.opts.Now, Deadline: deadline * reportDeadlines}
	rep, meta, err := assistant.WriteReport(ctx, run, msgs, assistant.Sink{
		Delta:    func(d assistant.Delta) error { return sse.send("delta", d) },
		Activity: func(a assistant.Activity) error { return sse.send("activity", a) },
	})
	keepCtx := context.WithoutCancel(ctx)
	if err := s.credits.RecordRun(keepCtx, user.ID, assistant.TurnReport, turn, meta); err != nil {
		slog.Error("usage not recorded", "user", user.ID, "err", err)
	}
	var re *assistant.RunError
	switch {
	case err == nil:
		kept, err := s.courses.AddReport(keepCtx, user, id, course.NewReport{
			Title: rep.Title, Summary: rep.Summary, Body: rep.Body, Points: rep.Points,
			References: refs, TurnIDs: taken, Meta: newTurnMeta(turn.Model, assistant.ReportPrompt, meta),
		})
		if err != nil {
			slog.Error("report not kept", "user", user.ID, "err", err)
			_ = sse.send("error", runError(assistant.CodeProviderError, false))
			return
		}
		_ = sse.send("done", kept)
	case errors.As(err, &re):
		slog.Info("report failed", "code", re.Code, "err", re.Cause)
		_ = sse.send("error", runError(re.Code, re.Kept))
	case ctx.Err() == nil:
		slog.Error("report failed", "err", err)
		_ = sse.send("error", runError(assistant.CodeProviderError, false))
	}
}

// reportMessages assembles a report's request (ADR 0012): the background
// as a follow-up reads it, then every node with all its turns. It returns
// too the turns taken in, in the order they were made, and the references:
// what those turns looked up, each place once.
func (s *Server) reportMessages(ctx context.Context, viewer auth.User, d course.ExplorationDetail) ([]assistant.Message, []int64, []course.Reference, error) {
	raw, err := s.courses.Snapshot(ctx, viewer, d.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	var snap analysisSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, nil, nil, err
	}
	rc := assistant.ReportContext{Title: d.Title, FirstQuestion: firstQuestion(d)}
	for _, m := range snap.Messages {
		rc.Snapshot = append(rc.Snapshot, assistant.Message{Role: m.Role, Content: m.Content})
	}
	// the main node first, then each node after the one it forked from
	at := map[int64]int{}
	turnAt := map[int64]int{}
	for _, n := range d.Nodes {
		for i, t := range n.Turns {
			turnAt[t.ID] = i + 1
		}
	}
	var taken []int64
	var turns []course.Turn
	for _, n := range d.Nodes {
		rn := assistant.ReportNode{Kind: string(n.Kind), Name: n.Name, Parent: -1}
		if n.ParentID != nil {
			rn.Parent = at[*n.ParentID]
			if n.ForkTurnID != nil {
				rn.Fork = turnAt[*n.ForkTurnID]
			}
		}
		list := n.Turns
		if n.Kind == course.NodeMain && len(list) > 0 {
			rc.FirstReply = list[0].Reply
			list = list[1:]
		}
		for _, t := range list {
			rn.Turns = append(rn.Turns, assistant.Exchange{Asked: asked(t.Question), Reply: t.Reply})
		}
		at[n.ID] = len(rc.Nodes)
		rc.Nodes = append(rc.Nodes, rn)
		turns = append(turns, n.Turns...)
	}
	seen := map[string]bool{}
	var refs []course.Reference
	for _, t := range sortedTurns(turns) {
		taken = append(taken, t.ID)
		for _, raw := range t.Meta.Sources {
			var src assistant.WebSource
			if json.Unmarshal(raw, &src) != nil || !webURL(src.URL) || seen[src.URL] {
				continue
			}
			seen[src.URL] = true
			refs = append(refs, course.Reference{Title: src.Title, URL: src.URL})
		}
	}
	return rc.Messages(), taken, refs, nil
}

// sortedTurns is turns in the order they were made.
func sortedTurns(turns []course.Turn) []course.Turn {
	out := slices.Clone(turns)
	slices.SortFunc(out, func(a, b course.Turn) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// webURL tells whether s is a link to the web, as a reference must be.
func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	list, err := s.courses.Reports(r.Context(), currentUser(r), id)
	if !writeReportError(w, r, err) {
		return
	}
	if list == nil {
		list = []course.ReportVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": list})
}

// reportParam reads the report a request names by its exploration and
// version.
func (s *Server) reportParam(w http.ResponseWriter, r *http.Request) (course.Report, bool) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return course.Report{}, false
	}
	v, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || v <= 0 {
		writeReportError(w, r, course.ErrReportNotFound)
		return course.Report{}, false
	}
	rep, err := s.courses.Report(r.Context(), currentUser(r), id, v)
	return rep, writeReportError(w, r, err)
}

func (s *Server) showReport(w http.ResponseWriter, r *http.Request) {
	if rep, ok := s.reportParam(w, r); ok {
		writeJSON(w, http.StatusOK, rep)
	}
}

// downloadReport is a version of the report as a Markdown file.
func (s *Server) downloadReport(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.reportParam(w, r)
	if !ok {
		return
	}
	name := fmt.Sprintf("%s-v%d.md", rep.Title, rep.Version)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-v%d.md"; filename*=UTF-8''%s`, rep.Version, url.PathEscape(name)))
	_, _ = w.Write([]byte(rep.Markdown()))
}
