package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// listConceptIndexes lists the library's courses with the status of their
// current version's concept index (#158).
func (s *Server) listConceptIndexes(w http.ResponseWriter, r *http.Request) {
	courses, err := s.courses.ConceptIndexes(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// showConceptIndex is the current version's index of a library course,
// with its entries once built.
func (s *Server) showConceptIndex(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	c, err := s.courses.Get(r.Context(), currentUser(r), id)
	if errors.Is(err, course.ErrNotFound) || err == nil && c.Visibility != course.Public {
		writeError(w, http.StatusNotFound, "course_not_found", "课程不存在")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	x, err := s.courses.ConceptIndexOf(r.Context(), id, c.FilesDir)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

// buildConceptIndex starts building the index of a library course's
// current version, from none or after it failed. The lessons are
// extracted one by one in the background, on the concept index's model,
// and charged to the site; the admin page follows the status.
func (s *Server) buildConceptIndex(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	turn, err := s.models.ForTurn(r.Context(), assistant.TurnConceptIndex)
	if !writeModelError(w, r, err) {
		return
	}
	b, err := s.courses.StartConceptIndex(r.Context(), id)
	switch {
	case errors.Is(err, course.ErrNotFound):
		writeError(w, http.StatusNotFound, "course_not_found", "课程不存在")
		return
	case errors.Is(err, course.ErrIndexBuilding):
		writeError(w, http.StatusConflict, "index_building", "这个版本的概念索引正在建立。")
		return
	case errors.Is(err, course.ErrIndexBuilt):
		writeError(w, http.StatusConflict, "index_built", "这个版本的概念索引已经建立。")
		return
	case err != nil:
		writeInternal(w, r, err)
		return
	}
	s.building.Add(1)
	go func() {
		defer s.building.Done()
		s.extractConcepts(s.buildCtx, turn, b)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": course.IndexBuilding})
}

// extractConcepts extracts the build's lessons in turn; the first that
// fails ends the build as failed, with what was extracted before it kept.
func (s *Server) extractConcepts(ctx context.Context, turn assistant.Turn, b course.IndexBuild) {
	failure := ""
	for _, l := range b.Lessons {
		sections := anchor.Sections(l.Markdown)
		titles := make([]string, len(sections))
		for i, sec := range sections {
			titles[i] = sec.Title
		}
		run := assistant.Run{Turn: turn, Now: s.opts.Now, Deadline: s.opts.RunDeadline}
		found, meta, err := assistant.ExtractConcepts(ctx, run, assistant.ConceptLesson{
			CourseTitle: b.CourseTitle, Chapter: l.Chapter, Title: l.Title, Sections: titles, Markdown: l.Markdown,
		})
		// the site's own run: no reader is charged (#158)
		if rerr := s.credits.RecordRun(context.WithoutCancel(ctx), 0, assistant.TurnConceptIndex, turn, meta); rerr != nil {
			slog.Error("usage not recorded", "kind", assistant.TurnConceptIndex, "err", rerr)
		}
		if err != nil {
			failure = assistant.CodeProviderError
			var re *assistant.RunError
			if errors.As(err, &re) {
				failure = re.Code
			}
			slog.Info("concept extraction failed", "course", b.CourseID, "lesson", l.Key, "err", err)
			break
		}
		if err := s.courses.KeepLessonConcepts(context.WithoutCancel(ctx), b, l, placed(found, sections)); err != nil {
			slog.Error("concepts not kept", "course", b.CourseID, "lesson", l.Key, "err", err)
			failure = "internal"
			break
		}
	}
	if err := s.courses.FinishConceptIndex(context.WithoutCancel(ctx), b, failure); err != nil {
		slog.Error("concept index not finished", "course", b.CourseID, "err", err)
	}
}

// placed puts each concept in the section whose heading it named; one
// naming no heading of the lesson is at the lesson's top, so its link
// still leads somewhere real.
func placed(found []assistant.Concept, sections []anchor.Section) []course.ExtractedConcept {
	out := make([]course.ExtractedConcept, 0, len(found))
	for _, c := range found {
		e := course.ExtractedConcept{Name: c.Name, Aliases: c.Aliases, Summary: c.Summary}
		for _, sec := range sections {
			if sec.Title == c.Section {
				e.SectionID, e.SectionTitle = sec.ID, sec.Title
				break
			}
		}
		out = append(out, e)
	}
	return out
}
