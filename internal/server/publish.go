package server

import (
	"net/http"

	"github.com/Bank5y/keine/internal/course"
)

// courseAction adapts a course operation on {id} into a handler that
// answers 204 on success.
func (s *Server) courseAction(op func(r *http.Request, id int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := courseIDParam(w, r)
		if !ok || !writeCourseError(w, r, op(r, id)) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) requestPublish(r *http.Request, id int64) error {
	return s.courses.RequestPublish(r.Context(), currentUser(r), id)
}

func (s *Server) withdrawPublish(r *http.Request, id int64) error {
	return s.courses.WithdrawPublish(r.Context(), currentUser(r), id)
}

func (s *Server) adminPublish(r *http.Request, id int64) error {
	return s.courses.Publish(r.Context(), currentUser(r), id)
}

func (s *Server) adminUnpublish(r *http.Request, id int64) error {
	return s.courses.Unpublish(r.Context(), currentUser(r), id)
}

func (s *Server) adminEditCourse(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var e course.Edit
	if !readJSON(w, r, &e) || !writeCourseError(w, r, s.courses.EditInfo(r.Context(), currentUser(r), id, e)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	list, err := s.courses.Reviews(r.Context(), currentUser(r))
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if list == nil {
		list = []course.Review{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": list})
}

func reviewIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return idParam(w, r, "review_not_found", "申请不存在或已处理")
}

func (s *Server) approveReview(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewIDParam(w, r)
	if !ok || !writeCourseError(w, r, s.courses.Approve(r.Context(), currentUser(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rejectReview(w http.ResponseWriter, r *http.Request) {
	id, ok := reviewIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) || !writeCourseError(w, r, s.courses.Reject(r.Context(), currentUser(r), id, in.Reason)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
