package server

import (
	"errors"
	"net/http"

	"github.com/Bank5y/keine/internal/course"
)

// The course editor's draft (ADR-0006): each user keeps at most one per
// course, as an ordered list of operations.

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	d, err := s.courses.GetDraft(r.Context(), currentUser(r), id)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *Server) addDraftOp(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var op course.Op
	if !readJSON(w, r, &op) {
		return
	}
	d, err := s.courses.AddDraftOp(r.Context(), currentUser(r), id, op)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *Server) undoDraftOp(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	d, err := s.courses.UndoDraftOp(r.Context(), currentUser(r), id)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *Server) discardDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	if writeDraftError(w, r, s.courses.DiscardDraft(r.Context(), currentUser(r), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) commitDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	res, err := s.courses.CommitDraft(r.Context(), currentUser(r), id)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusOK, res)
	}
}

// writeDraftError answers the errors of the draft endpoints and reports
// whether err was nil.
func writeDraftError(w http.ResponseWriter, r *http.Request, err error) bool {
	var rejected *course.OpRejected
	switch {
	case errors.As(err, &rejected):
		writeError(w, http.StatusUnprocessableEntity, "draft_op_rejected", rejected.Message)
	case errors.Is(err, course.ErrDraftEmpty):
		writeError(w, http.StatusConflict, "draft_empty", "编辑稿里还没有修改")
	case errors.Is(err, course.ErrUpdateNotAllowed):
		writeError(w, http.StatusForbidden, "update_not_allowed", "已完结的课程只有上传者可以提交更新")
	default:
		return writeCourseError(w, r, err)
	}
	return false
}
