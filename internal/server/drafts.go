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
	// skip: the indexes of the conflicts the user confirmed to skip
	var body struct {
		Skip []int `json:"skip"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	res, err := s.courses.CommitDraft(r.Context(), currentUser(r), id, body.Skip)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusOK, res)
	}
}

// writeDraftError answers the errors of the draft endpoints and reports
// whether err was nil.
func writeDraftError(w http.ResponseWriter, r *http.Request, err error) bool {
	var rejected *course.OpRejected
	switch {
	case errors.As(err, &rejected) && len(rejected.Problems) > 0:
		writeProblems(w, http.StatusUnprocessableEntity, "draft_op_rejected", rejected.Message, rejected.Problems)
	case errors.As(err, &rejected):
		writeError(w, http.StatusUnprocessableEntity, "draft_op_rejected", rejected.Message)
	case isUploadNotFound(err):
		writeError(w, http.StatusNotFound, "draft_upload_not_found", "上传的文件已不在编辑稿里, 请重新上传")
	case errors.Is(err, course.ErrDraftEmpty):
		writeError(w, http.StatusConflict, "draft_empty", "编辑稿里还没有修改")
	case errors.Is(err, course.ErrDraftRequestUnsupported):
		writeError(w, http.StatusConflict, "draft_request_unsupported", "暂不支持把编辑稿提交为更新申请; 请先用「提交更新」上传课程包")
	case errors.Is(err, course.ErrUpdateNotAllowed):
		writeError(w, http.StatusForbidden, "update_not_allowed", "已完结的课程只有上传者可以提交更新")
	default:
		return writeCourseError(w, r, err)
	}
	return false
}
