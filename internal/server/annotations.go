package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Bank5y/keine/internal/course"
)

// writeAnnotationError answers the annotation errors and reports whether err
// was nil.
func writeAnnotationError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, course.ErrAnnotationNotFound):
		writeError(w, http.StatusNotFound, "annotation_not_found", "标注不存在")
	case errors.Is(err, course.ErrNotAnnotatable):
		writeError(w, http.StatusConflict, "not_annotatable", "这一讲不能做标注")
	case errors.Is(err, course.ErrQuoteNotFound):
		writeError(w, http.StatusUnprocessableEntity, "quote_not_found", "正文里找不到选中的文字")
	default:
		return writeCourseError(w, r, err)
	}
	return false
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	list, err := s.courses.Annotations(r.Context(), currentUser(r), id, lessonID)
	if !writeAnnotationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"annotations": list})
}

func (s *Server) createAnnotation(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	var in course.NewAnnotation
	if !readJSON(w, r, &in) {
		return
	}
	a, err := s.courses.Annotate(r.Context(), currentUser(r), id, lessonID, in)
	if !writeAnnotationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func annotationIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return idParam(w, r, "annotation_not_found", "标注不存在")
}

func (s *Server) restyleAnnotation(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationIDParam(w, r)
	if !ok {
		return
	}
	var in course.AnnotationStyle
	if !readJSON(w, r, &in) {
		return
	}
	if !writeAnnotationError(w, r, s.courses.Restyle(r.Context(), currentUser(r), id, in)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationIDParam(w, r)
	if !ok {
		return
	}
	if !writeAnnotationError(w, r, s.courses.DeleteAnnotation(r.Context(), currentUser(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeNote(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !writeAnnotationError(w, r, s.courses.WriteNote(r.Context(), currentUser(r), id, in.Note)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationIDParam(w, r)
	if !ok {
		return
	}
	if !writeAnnotationError(w, r, s.courses.DeleteNote(r.Context(), currentUser(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) myAnnotations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f course.NoteFilter
	if v := q.Get("course"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "筛选条件不合法")
			return
		}
		f.CourseID = id
	}
	f.Style, f.Color, f.Query, f.Sort = q.Get("style"), q.Get("color"), q.Get("q"), q.Get("sort")
	switch q.Get("noted") {
	case "", "0", "false":
	case "1", "true":
		f.Noted = true
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "筛选条件不合法")
		return
	}
	nb, err := s.courses.Notebook(r.Context(), currentUser(r), f)
	if !writeAnnotationError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, nb)
}
