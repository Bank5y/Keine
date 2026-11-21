package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// analysisTTL is how long an analysis can be pinned after it is done (#118).
const analysisTTL = 30 * time.Minute

// stagedAnalysis is a finished analysis not yet pinned: what pinning
// keeps, and where and until when it can be pinned. It lives in memory
// only; an analysis not pinned leaves no record (#123).
type stagedAnalysis struct {
	id                 string
	courseID, lessonID int64
	quote              anchor.Quote
	analysis           assistant.Analysis
	snapshot           analysisSnapshot
	meta               turnMeta
	until              time.Time
}

// analysisSnapshot is the request an analysis ran on, as an exploration
// keeps it for the turns after it (versioned JSON).
type analysisSnapshot struct {
	Version int    `json:"version"`
	Prompt  string `json:"prompt"`
	// LessonText is how much of the lesson went in: "full" or "related".
	LessonText string            `json:"lessonText"`
	Messages   []snapshotMessage `json:"messages"`
}

type snapshotMessage struct {
	Role    assistant.Role `json:"role"`
	Content string         `json:"content"`
}

// turnMeta is what a turn records besides its reply (ADR 0008).
type turnMeta struct {
	Source    string                `json:"source"`
	Model     string                `json:"model"`
	Prompt    string                `json:"prompt"`
	Usage     assistant.Usage       `json:"usage"`
	Estimated bool                  `json:"estimated,omitempty"`
	Searches  int                   `json:"searches"`
	Sources   []assistant.WebSource `json:"sources"`
	Degraded  []string              `json:"degraded,omitempty"`
	// Interrupted is why the reply stops short, "" when it is whole (#136)
	Interrupted course.Interruption `json:"interrupted,omitempty"`
}

// stagedAnalyses keeps each reader's latest finished analysis, one each: a
// new analysis replaces the one before as soon as it starts.
type stagedAnalyses struct {
	mu     sync.Mutex
	byUser map[int64]stagedAnalysis
}

func newStagedAnalyses() *stagedAnalyses {
	return &stagedAnalyses{byUser: map[int64]stagedAnalysis{}}
}

// drop throws away a reader's analysis, as a new one starts.
func (s *stagedAnalyses) drop(user int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byUser, user)
}

// keep stages a finished analysis under a new ID, which it returns.
func (s *stagedAnalyses) keep(user int64, a stagedAnalysis) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	a.id = hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byUser[user] = a
	return a.id
}

// find is the reader's analysis by its ID, on the given lesson, while it
// can still be pinned.
func (s *stagedAnalyses) find(user int64, id string, courseID, lessonID int64, now time.Time) (stagedAnalysis, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byUser[user]
	if !ok || id == "" || a.id != id || a.courseID != courseID || a.lessonID != lessonID {
		return stagedAnalysis{}, false
	}
	if !now.Before(a.until) {
		delete(s.byUser, user)
		return stagedAnalysis{}, false
	}
	return a, true
}

// forget forgets an analysis once it is an exploration.
func (s *stagedAnalyses) forget(user int64, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byUser[user].id == id {
		delete(s.byUser, user)
	}
}

// writeExplorationError answers the exploration errors and reports whether
// err was nil.
func writeExplorationError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, course.ErrOverlap):
		writeError(w, http.StatusConflict, "overlap", "这段文字已有探索。")
	case errors.Is(err, course.ErrExplorationNotFound):
		writeError(w, http.StatusNotFound, "exploration_not_found", "探索不存在。")
	case errors.Is(err, assistant.ErrAnalysisExpired):
		writeError(w, http.StatusGone, "analysis_expired", "分析结束已超过 30 分钟，或者你又开始了新的分析。重新分析后再固定。")
	default:
		return writeAnnotationError(w, r, err)
	}
	return false
}

func (s *Server) listExplorations(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	list, err := s.courses.Explorations(r.Context(), currentUser(r), id, lessonID)
	if !writeExplorationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"explorations": list})
}

// pinExploration keeps the reader's finished analysis as an exploration
// (#123): the request names the analysis by its ID and nothing else.
func (s *Server) pinExploration(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	var in struct {
		AnalysisID string `json:"analysisId"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	user := currentUser(r)
	a, ok := s.staged.find(user.ID, in.AnalysisID, id, lessonID, s.opts.Now())
	if !ok {
		writeExplorationError(w, r, assistant.ErrAnalysisExpired)
		return
	}
	e, err := s.courses.Pin(r.Context(), user, id, lessonID, course.Pin{
		Quote: a.quote, Title: a.analysis.Title, Intro: a.analysis.Intro, Meaning: a.analysis.Body,
		Snapshot: a.snapshot, Reply: a.analysis.Reply(), Meta: a.meta,
	})
	if !writeExplorationError(w, r, err) {
		return
	}
	s.staged.forget(user.ID, in.AnalysisID)
	writeJSON(w, http.StatusCreated, e)
}

func explorationIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return idParam(w, r, "exploration_not_found", "探索不存在。")
}

// showExploration is one of the reader's explorations whole: its document
// and its dialogue (#134).
func (s *Server) showExploration(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	d, err := s.courses.ExplorationDetail(r.Context(), currentUser(r), id)
	if !writeExplorationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteExploration(w http.ResponseWriter, r *http.Request) {
	id, ok := explorationIDParam(w, r)
	if !ok {
		return
	}
	if !writeExplorationError(w, r, s.courses.DeleteExploration(r.Context(), currentUser(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
