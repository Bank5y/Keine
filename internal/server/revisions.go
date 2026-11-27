package server

import (
	"net/http"
	"strconv"

	"github.com/Bank5y/keine/internal/course"
)

// The admin's revisions of a library course's concept index (#226): they
// belong to the course and are laid over recall, so they take effect at
// once.

// listRevisions is the course's revisions, each marked stale when what it
// points at changed in the current version.
func (s *Server) listRevisions(w http.ResponseWriter, r *http.Request) {
	c, ok := s.libraryCourse(w, r)
	if !ok {
		return
	}
	revs, err := s.courses.RevisionsOf(r.Context(), c.ID)
	if !writeCourseError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

// listIndexSections is the lessons of the course's current version with
// their sections, for a revision to point at.
func (s *Server) listIndexSections(w http.ResponseWriter, r *http.Request) {
	c, ok := s.libraryCourse(w, r)
	if !ok {
		return
	}
	lessons, err := s.courses.SectionsOf(r.Context(), c.ID)
	if !writeCourseError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lessons": lessons})
}

// reviseConcept keeps a revision of a concept, by its name as extracted.
func (s *Server) reviseConcept(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Concept string `json:"concept"`
		course.ConceptRevision
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !writeCourseError(w, r, s.courses.ReviseConcept(r.Context(), id, body.Concept, body.ConceptRevision)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addConcept keeps a concept the admin adds at a section.
func (s *Server) addConcept(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var body course.NewConcept
	if !readJSON(w, r, &body) {
		return
	}
	rid, err := s.courses.AddConcept(r.Context(), id, body)
	if !writeCourseError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": rid})
}

// excludeFromRecall leaves a lesson or a section out of recall.
func (s *Server) excludeFromRecall(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var body struct {
		LessonKey string `json:"lessonKey"`
		SectionID string `json:"sectionId"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	rid, err := s.courses.Exclude(r.Context(), id, body.LessonKey, body.SectionID)
	if !writeCourseError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": rid})
}

func revisionIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "revision_not_found", "这条修订不存在")
		return 0, false
	}
	return rid, true
}

// deleteRevision takes a revision back.
func (s *Server) deleteRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	rid, ok := revisionIDParam(w, r)
	if !ok || !writeCourseError(w, r, s.courses.DeleteRevision(r.Context(), id, rid)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// confirmRevision has the admin confirm a revision that may be outdated.
func (s *Server) confirmRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	rid, ok := revisionIDParam(w, r)
	if !ok || !writeCourseError(w, r, s.courses.ConfirmRevision(r.Context(), id, rid)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
