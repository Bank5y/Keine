package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/credits"
)

// errCreditsOut cancels a run that spent what its reader had left today.
var errCreditsOut = errors.New("credits exhausted")

// turnRequest is a question of the dialogue: its text and what it attaches.
type turnRequest struct {
	Question course.Question `json:"question"`
}

func writeDialogueError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, course.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "node_not_found", "节点不存在。")
	case errors.Is(err, course.ErrTurnNotFound):
		writeError(w, http.StatusNotFound, "turn_not_found", "这一轮不存在。")
	case errors.Is(err, course.ErrNotLastTurn):
		writeError(w, http.StatusConflict, "not_last_turn", "节点里只能接着最后一轮往下问；要从这一轮问别的，请开一个节点。")
	case errors.Is(err, course.ErrMainNode):
		writeError(w, http.StatusConflict, "main_node", "主节点不能删除。")
	case errors.Is(err, course.ErrNodeLimit):
		writeError(w, http.StatusConflict, "node_limit", "这份探索已有 50 个节点，删除一些后才能再开。")
	case errors.Is(err, course.ErrNotRetriable):
		writeError(w, http.StatusConflict, "not_retriable", "只有最后一轮没有完成时才能重试。")
	default:
		return writeExplorationError(w, r, err)
	}
	return false
}

// askTurn asks a question after the last turn of a node (#136): the reply
// streams and is kept, whole or as far as it got.
func (s *Server) askTurn(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		turnRequest
		After int64 `json:"after"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !course.ValidQuestion(in.Question) {
		writeError(w, http.StatusBadRequest, "bad_request", "请写下要问的问题。")
		return
	}
	user := currentUser(r)
	d, err := s.courses.ExplorationDetail(r.Context(), user, id)
	if !writeDialogueError(w, r, err) {
		return
	}
	if !s.converse(w, r, d, in.After, asked(in.Question), nil, func(ctx context.Context, reply string, meta turnMeta) (any, error) {
		return s.courses.AddTurn(ctx, user, id, in.After, course.NewTurn{Question: in.Question, Reply: reply, Meta: meta})
	}, func() error {
		// checked before the run too, so a question after a turn that is not
		// the last is turned away rather than answered and dropped
		return d.AfterLast(in.After)
	}) {
		return
	}
}

// openNode opens a node on a turn and asks its first question (#137).
func (s *Server) openNode(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		turnRequest
		Kind course.NodeKind `json:"kind"`
		Fork int64           `json:"fork"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !course.ValidQuestion(in.Question) || !in.Kind.Openable() {
		writeError(w, http.StatusBadRequest, "bad_request", "请选择节点类型并写下第一个问题。")
		return
	}
	user := currentUser(r)
	d, err := s.courses.ExplorationDetail(r.Context(), user, id)
	if !writeDialogueError(w, r, err) {
		return
	}
	s.converse(w, r, d, in.Fork, forked(in.Kind, in.Question), s.linksFor(user, d), func(ctx context.Context, reply string, meta turnMeta) (any, error) {
		return s.courses.OpenNode(ctx, user, id, in.Fork, in.Kind, course.NewTurn{Question: in.Question, Reply: reply, Meta: meta})
	}, func() error {
		if _, ok := d.Chain(in.Fork); !ok {
			return course.ErrTurnNotFound
		}
		if len(d.Nodes) >= course.MaxNodes {
			return course.ErrNodeLimit
		}
		return nil
	})
}

// retryTurn asks a turn that failed or stopped short again and writes the
// new reply over it, in place (#136).
func (s *Server) retryTurn(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	turnID, err := strconv.ParseInt(r.PathValue("turn"), 10, 64)
	if err != nil {
		writeDialogueError(w, r, course.ErrTurnNotFound)
		return
	}
	user := currentUser(r)
	d, err := s.courses.ExplorationDetail(r.Context(), user, id)
	if !writeDialogueError(w, r, err) {
		return
	}
	target, err := d.Retriable(turnID)
	if !writeDialogueError(w, r, err) {
		return
	}
	// it reads what it read the first time: up to the turn before it, or
	// to the turn its node forked from
	var before int64
	switch {
	case target.Turn.ParentID != nil:
		before = *target.Turn.ParentID
	case target.Node.ForkTurnID != nil:
		before = *target.Node.ForkTurnID
	default:
		writeDialogueError(w, r, course.ErrNotRetriable) // the analysis is never asked again
		return
	}
	// a node's first turn tried again is the turn it forks with still
	var links *courseLinks
	if target.Turn.ParentID == nil {
		links = s.linksFor(user, d)
	}
	s.converse(w, r, d, before, askedIn(target.Turn), links, func(ctx context.Context, reply string, meta turnMeta) (any, error) {
		return s.courses.ReplaceTurn(ctx, user, id, turnID, reply, meta)
	}, nil)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	node, err := strconv.ParseInt(r.PathValue("node"), 10, 64)
	if err != nil {
		writeDialogueError(w, r, course.ErrNodeNotFound)
		return
	}
	if !writeDialogueError(w, r, s.courses.DeleteNode(r.Context(), currentUser(r), id, node)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// keep saves a turn's reply as far as it got with its metadata and returns
// what the done or error event carries (the turn, or the new node).
type keep func(ctx context.Context, reply string, meta turnMeta) (any, error)

// converse runs a turn of the dialogue after turn `after` (ADR 0008,
// #136): the follow-up skill on the follow-up turn's model, with the
// analysis's tools and limits, its context assembled from the
// exploration (FollowupContext). It is checked before it starts (check,
// then the context's size and the reader's credits); once it starts, the
// reply streams as Server-Sent Events and is kept by keep however it
// ends: done carries what was kept, error says why the reply stops short
// and carries it too. A run that spends the reader's last credits is
// cancelled and kept as interrupted. With links (the turn a node forks
// with), the answer is linked to the course after it is written (ADR 0011).
func (s *Server) converse(w http.ResponseWriter, r *http.Request, d course.ExplorationDetail, after int64, q assistant.Asked, links *courseLinks, save keep, check func() error) bool {
	ctx := r.Context()
	user := currentUser(r)
	if check != nil && !writeDialogueError(w, r, check()) {
		return false
	}
	turn, err := s.models.ForTurn(ctx, assistant.TurnFollowup)
	if !writeModelError(w, r, err) {
		return false
	}
	msgs, err := s.followupMessages(ctx, user, d, after, q)
	if !writeDialogueError(w, r, err) {
		return false
	}
	if assistant.EstimateMessages(msgs) > assistant.LimitsOf(turn).Input {
		writeError(w, http.StatusUnprocessableEntity, "context_too_long", "这个节点的对话太长，模型容纳不下。可以在更早的一轮上开一个节点，从那里接着问。")
		return false
	}
	// a turn of the dialogue is not estimated: some credits left is enough (#123)
	account, err := s.credits.Account(ctx, user.ID)
	if err != nil {
		writeInternal(w, r, err)
		return false
	}
	if account.Remaining != nil && *account.Remaining <= 0 {
		writeCreditsError(w, r, &credits.InsufficientError{Remaining: *account.Remaining})
		return false
	}
	c, err := s.courses.Get(ctx, user, d.CourseID)
	if !writeAnnotationError(w, r, err) {
		return false
	}
	sse, leave, ok := s.streamRun(w, r, user.ID)
	if !ok {
		return false
	}
	defer leave()

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	run := assistant.Run{
		Turn:     turn,
		Tools:    append(assistant.CourseTools(&courseLibrary{s.courses, user, c}), s.opts.ExtraTools...),
		Web:      turn.Web,
		Now:      s.opts.Now,
		Deadline: s.opts.RunDeadline,
		Spent: func(u assistant.Usage) {
			if account.Remaining != nil && credits.Cost(u, 0, turn.Rates) >= *account.Remaining {
				cancel(errCreditsOut)
			}
		},
	}
	var linker assistant.Linker
	if links != nil {
		linker = links.linker(ctx, c)
	}
	out, meta, err := assistant.Followup(runCtx, run, msgs, assistant.Sink{
		Delta:    func(d assistant.Delta) error { return sse.send("delta", d) },
		Activity: func(a assistant.Activity) error { return sse.send("activity", a) },
	}, linker)
	reply := out.Text
	s.recordRun(ctx, user.ID, assistant.TurnFollowup, turn, meta)
	keepCtx := context.WithoutCancel(ctx)
	tm := newTurnMeta(turn.Model, assistant.FollowupPrompt, meta)
	tm.NodeNote = q.Note
	if links != nil {
		tm.CourseLinks = links.chosen(out.Links)
	}
	code := ""
	switch {
	case err == nil:
		// a reply that finished as the credits ran out is whole
	case errors.Is(context.Cause(runCtx), errCreditsOut):
		tm.Interrupted, code = course.InterruptedCredits, string(course.InterruptedCredits)
	default:
		// the reader going away or the server failing keeps the question too
		code, _ = runFailure(ctx, "follow-up turn", err)
		switch code {
		case assistant.CodeTimeout:
			tm.Interrupted = course.InterruptedTimeout
		case assistant.CodeOutputTruncated:
			tm.Interrupted = course.InterruptedTruncated
		default:
			tm.Interrupted = course.InterruptedFailed
		}
	}
	kept, kerr := save(keepCtx, reply, tm)
	if kerr != nil {
		slog.Error("turn not kept", "user", user.ID, "err", kerr)
		_ = sse.send("error", runError(assistant.CodeProviderError, false))
		return true
	}
	if code == "" {
		_ = sse.send("done", map[string]any{"result": kept})
		return true
	}
	ev := runError(code, reply != "")
	if code == string(course.InterruptedCredits) {
		ev["message"] = "今天的积分已经用完，回复在这里中断了。积分在每天 0 点重置。"
	}
	ev["result"] = kept
	_ = sse.send("error", ev)
	return true
}

// maxCandidates is how many concepts an answer's linking step looks at.
const maxCandidates = 5

// courseLinks links the answer of the turn a node forks with to the
// course (ADR 0011), and remembers the entries it offered the model.
type courseLinks struct {
	s       *Server
	viewer  auth.User
	d       course.ExplorationDetail
	offered []course.ConceptEntry
}

func (s *Server) linksFor(viewer auth.User, d course.ExplorationDetail) *courseLinks {
	return &courseLinks{s: s, viewer: viewer, d: d}
}

// linker finds, in an answer, the concepts of the course's other lessons,
// by the index of the version the reader reads: none when it has no index
// built.
func (l *courseLinks) linker(ctx context.Context, c course.Detail) assistant.Linker {
	x, err := l.s.courses.ConceptIndexOf(ctx, l.d.CourseID, c.FilesDir)
	if err != nil {
		slog.Error("concept index not read", "course", l.d.CourseID, "err", err)
		return nil
	}
	if x.Status != course.ConceptIndexBuilt {
		return nil
	}
	lib := &courseLibrary{l.s.courses, l.viewer, c}
	return func(ctx context.Context, answer string) ([]assistant.LinkCandidate, error) {
		var out []assistant.LinkCandidate
		for _, e := range x.Mentioned(answer, l.d.LessonKey, maxCandidates) {
			text, err := lib.Text(ctx, e.Lesson.Key)
			if err != nil {
				// a lesson the reader cannot read now is not linked
				continue
			}
			lc := assistant.LinkCandidate{Concept: e.Name, Lesson: e.Lesson.Title, Text: text}
			if e.Section != nil {
				lc.Section = e.Section.Title
				for _, sec := range anchor.Sections(text) {
					if sec.ID == e.Section.ID {
						lc.Text = text[sec.Start:sec.End]
						break
					}
				}
			}
			l.offered = append(l.offered, e)
			out = append(out, lc)
		}
		return out, nil
	}
}

// chosen is the course links the model picked of the entries offered.
func (l *courseLinks) chosen(picked []assistant.CourseLink) []course.CourseLink {
	var out []course.CourseLink
	for _, p := range picked {
		e := l.offered[p.Candidate]
		out = append(out, course.CourseLink{Concept: e.Name, Note: p.Note, Lesson: e.Lesson, Section: e.Section})
	}
	return out
}

// followupMessages assembles a question's context (ADR 0011): the
// background from the analysis's snapshot, the first exchange, and the
// dialogue along the question's chain after the analysis.
func (s *Server) followupMessages(ctx context.Context, viewer auth.User, d course.ExplorationDetail, after int64, q assistant.Asked) ([]assistant.Message, error) {
	chain, ok := d.Chain(after)
	if !ok || len(chain) == 0 {
		return nil, course.ErrTurnNotFound
	}
	raw, err := s.courses.Snapshot(ctx, viewer, d.ID)
	if err != nil {
		return nil, err
	}
	var snap analysisSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	fc := assistant.FollowupContext{FirstQuestion: firstQuestion(d), FirstReply: chain[0].Reply}
	for _, m := range snap.Messages {
		fc.Snapshot = append(fc.Snapshot, assistant.Message{Role: m.Role, Content: m.Content})
	}
	for _, t := range chain[1:] {
		fc.History = append(fc.History, assistant.Exchange{Asked: askedIn(t), Reply: withLinks(t)})
	}
	return fc.Messages(q), nil
}

// withLinks is a kept turn's reply as the reader saw it: the answer, then
// what it linked of the course, if anything (ADR 0011).
func withLinks(t course.Turn) string {
	if len(t.CourseLinks) == 0 {
		return t.Reply
	}
	var b strings.Builder
	b.WriteString(t.Reply + "\n\n课程里的相关内容：")
	for _, l := range t.CourseLinks {
		where := "讲「" + l.Lesson.Title + "」"
		if l.Section != nil {
			where += " › 小节「" + l.Section.Title + "」"
		}
		b.WriteString("\n- " + where + "：" + l.Note)
	}
	return b.String()
}

// firstQuestion is the question an exploration's analysis stands for.
func firstQuestion(d course.ExplorationDetail) string {
	return assistant.FirstQuestion(d.Quote, d.Title)
}

func asked(q course.Question) assistant.Asked {
	return assistant.Asked{Text: q.Text, Fragments: q.Selections()}
}

// forked is the question a node of kind forks with: headed by the kind's
// note (ADR 0011).
func forked(kind course.NodeKind, q course.Question) assistant.Asked {
	a := asked(q)
	a.Note = assistant.NodeNote(string(kind))
	return a
}

// askedIn is what a kept turn asked, as it was asked: the turn a node
// forked with (its node's first) headed by the note kept with it, or its
// kind's note when it was kept before notes were.
func askedIn(t course.Turn) assistant.Asked {
	if t.ParentID == nil {
		a := forked(t.NodeKind, t.Question)
		if t.NodeNote != "" {
			a.Note = t.NodeNote
		}
		return a
	}
	return asked(t.Question)
}
