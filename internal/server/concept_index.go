package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/embedding"
)

// indexExtractor is what extracts the concept index now: the concept
// index's turn and, for the indexes to be compared with, its prompt version
// and model. Without a model set up, the turn's error comes back with the
// prompt version alone.
func (s *Server) indexExtractor(ctx context.Context) (assistant.Turn, course.Extractor, error) {
	ex := course.Extractor{PromptVersion: assistant.ConceptIndexPrompt.Version}
	turn, err := s.models.ForTurn(ctx, assistant.TurnConceptIndex)
	if err == nil {
		ex.Model, ex.ModelName = turn.Model, turn.DisplayName
	}
	return turn, ex, err
}

// listConceptIndexes lists the library's courses with the status of their
// current version's concept index (#158), and whether it is stale.
func (s *Server) listConceptIndexes(w http.ResponseWriter, r *http.Request) {
	courses, err := s.courses.ConceptIndexes(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	_, cur, _ := s.indexExtractor(r.Context())
	for i := range courses {
		courses[i].Stale = course.StaleFor(courses[i].Status, courses[i].Extractor, cur)
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// showConceptIndex is the current version's index of a library course:
// its status, the progress of its lessons and the entries so far.
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
	_, cur, _ := s.indexExtractor(r.Context())
	x.Stale = course.StaleFor(x.Status, x.Extractor, cur)
	if emb, err := s.embedding.Current(r.Context()); err == nil && emb.Provider != embedding.Off {
		p, err := s.courses.EmbeddingProgressFor(r.Context(), id, c.FilesDir, emb.Generation)
		if err != nil {
			writeInternal(w, r, err)
			return
		}
		x.Embedding = &p
	}
	writeJSON(w, http.StatusOK, x)
}

// recallConceptIndex is the recall test (#222): what the linking step
// would be offered for a text in the current version of a library
// course, leaving out a lesson (the selection's own), with each recall
// path's ranking. It calls no model; with embedding on, it embeds the
// text, charged to the site like any query.
func (s *Server) recallConceptIndex(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Text          string `json:"text"`
		ExcludeLesson string `json:"excludeLesson"`
	}
	if !readJSON(w, r, &req) {
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
	rc, err := s.recallOf(r.Context(), id, c.FilesDir)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rc.Trace(r.Context(), req.Text, req.ExcludeLesson))
}

// buildConceptIndex starts building the index of a library course's
// current version: from none, or again. The lessons are extracted in the
// background, a few at a time, on the concept index's model, and charged
// to the site; the admin page follows the progress.
func (s *Server) buildConceptIndex(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	turn, ex, err := s.indexExtractor(r.Context())
	if !writeModelError(w, r, err) {
		return
	}
	b, err := s.courses.StartConceptIndex(r.Context(), id, ex)
	if !writeCourseError(w, r, err) {
		return
	}
	s.extractInBackground(turn, b)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": course.ConceptIndexBuilding})
}

// retryConceptLesson extracts one lesson of a library course's current
// index again (#224), in the background, as a build of that lesson alone.
func (s *Server) retryConceptLesson(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	turn, ex, err := s.indexExtractor(r.Context())
	if !writeModelError(w, r, err) {
		return
	}
	b, err := s.courses.RetryConceptLesson(r.Context(), id, r.PathValue("key"), ex)
	if !writeCourseError(w, r, err) {
		return
	}
	s.extractInBackground(turn, b)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": course.ConceptIndexBuilding})
}

func (s *Server) extractInBackground(turn assistant.Turn, b course.ConceptIndexBuild) {
	s.building.Add(1)
	go func() {
		defer s.building.Done()
		s.extractConcepts(s.buildCtx, turn, b)
	}()
}

// extractingAtOnce is how many lessons of a build are extracted side by
// side (#224).
const extractingAtOnce = 4

// extractConcepts extracts the build's lessons, extractingAtOnce at a
// time; one that fails is recorded with its reason and the rest go on.
func (s *Server) extractConcepts(ctx context.Context, turn assistant.Turn, b course.ConceptIndexBuild) {
	slots := make(chan struct{}, extractingAtOnce)
	var wg sync.WaitGroup
	for _, l := range b.Lessons {
		select {
		case <-ctx.Done():
		case slots <- struct{}{}:
			wg.Add(1)
			go func() {
				defer func() { <-slots; wg.Done() }()
				s.extractLesson(ctx, turn, b, l)
			}()
			continue
		}
		break
	}
	wg.Wait()
	s.summarizeIndex(ctx, turn, b)
	if err := s.courses.FinishConceptIndex(context.WithoutCancel(ctx), b); err != nil {
		slog.Error("concept index not finished", "course", b.CourseID, "err", err)
	}
}

// extractLesson extracts one lesson of the build and keeps what it found,
// or why it failed.
func (s *Server) extractLesson(ctx context.Context, turn assistant.Turn, b course.ConceptIndexBuild, l course.ConceptIndexLesson) {
	keep := context.WithoutCancel(ctx)
	if err := s.courses.RunLessonConcepts(keep, b, l); err != nil {
		slog.Error("lesson extraction not started", "course", b.CourseID, "lesson", l.Key, "err", err)
		return
	}
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
	s.recordRun(ctx, 0, assistant.TurnConceptIndex, turn, meta)
	if err == nil {
		err = s.courses.KeepLessonConcepts(keep, b, l, found.Summary, placed(found.Concepts, sections))
		if err != nil {
			slog.Error("concepts not kept", "course", b.CourseID, "lesson", l.Key, "err", err)
			err = &assistant.RunError{Code: "internal", Cause: err}
		}
	}
	if err != nil {
		code, _ := runFailure(ctx, "concept extraction", err)
		if err := s.courses.FailLessonConcepts(keep, b, l, code); err != nil {
			slog.Error("lesson failure not kept", "course", b.CourseID, "lesson", l.Key, "err", err)
		}
	}
}

// placed puts each concept in the section whose heading it named; one
// naming no heading of the lesson is at the lesson's top, so its link
// still leads somewhere real.
func placed(found []assistant.Concept, sections []anchor.Section) []course.ExtractedConcept {
	out := make([]course.ExtractedConcept, 0, len(found))
	for _, c := range found {
		e := course.ExtractedConcept{Name: c.Name, Aliases: c.Aliases, Summary: c.Summary, Role: string(c.Role)}
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

// summarizeIndex is a build's course-level summary (#225), once its
// lessons are through: the entries merged into concepts, then a summary of
// each chapter from its lessons' summaries and one of the course from the
// chapters'. A summary whose input is what it was written from before is
// kept as it was; one that fails is recorded and the rest go on.
func (s *Server) summarizeIndex(ctx context.Context, turn assistant.Turn, b course.ConceptIndexBuild) {
	keep := context.WithoutCancel(ctx)
	if err := s.courses.SummarizingConceptIndex(keep, b); err != nil {
		slog.Error("concept index not summarized", "course", b.CourseID, "err", err)
		return
	}
	if err := s.courses.MergeConcepts(keep, b); err != nil {
		slog.Error("concepts not merged", "course", b.CourseID, "err", err)
	}
	tree, err := s.courses.SummaryPlan(keep, b)
	if err != nil {
		slog.Error("summary tree not read", "course", b.CourseID, "err", err)
		return
	}
	var chapters []assistant.SummaryPart
	for _, ch := range tree.Chapters {
		var parts []assistant.SummaryPart
		for _, l := range ch.Lessons {
			if l.Status == course.LessonDone && l.Summary != "" {
				parts = append(parts, assistant.SummaryPart{Title: l.Title, Summary: l.Summary})
			}
		}
		if len(parts) == 0 {
			continue
		}
		r := s.summarize(ctx, turn, assistant.SummaryInput{CourseTitle: b.CourseTitle, Chapter: ch.Title, Parts: parts}, ch.Summary, ch.Hash, ch.Error)
		r.TooLong = len(ch.Lessons) > course.MaxChapterLessons
		if err := s.courses.KeepChapterSummary(keep, b, ch.Key, r); err != nil {
			slog.Error("chapter summary not kept", "course", b.CourseID, "chapter", ch.Key, "err", err)
		}
		if r.Summary != "" {
			chapters = append(chapters, assistant.SummaryPart{Title: ch.Title, Summary: r.Summary})
		}
	}
	if len(chapters) == 0 {
		return
	}
	r := s.summarize(ctx, turn, assistant.SummaryInput{CourseTitle: b.CourseTitle, Parts: chapters}, tree.Summary, tree.Hash, tree.Error)
	if err := s.courses.KeepCourseSummary(keep, b, r); err != nil {
		slog.Error("course summary not kept", "course", b.CourseID, "err", err)
	}
}

// summarize writes one summary of the tree, unless the one there (was,
// written from wasHash, without error) was written from the same input by
// the same prompt and model. A request over the model's input limit is not
// sent. The site is charged.
func (s *Server) summarize(ctx context.Context, turn assistant.Turn, in assistant.SummaryInput, was, wasHash, wasErr string) course.SummaryResult {
	msgs := in.Messages()
	h := sha256.New()
	h.Write([]byte(assistant.SummaryPrompt.Version + "\x00" + turn.Model))
	for _, m := range msgs {
		h.Write([]byte("\x00" + m.Content))
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if hash == wasHash && wasErr == "" && was != "" {
		return course.SummaryResult{Summary: was, Hash: hash}
	}
	if ctx.Err() != nil {
		return course.SummaryResult{Error: "interrupted"}
	}
	if assistant.EstimateMessages(msgs) > assistant.LimitsOf(turn).Input {
		return course.SummaryResult{Error: "context_too_long"}
	}
	run := assistant.Run{Turn: turn, Now: s.opts.Now, Deadline: s.opts.RunDeadline}
	text, meta, err := assistant.Summarize(ctx, run, in)
	s.recordRun(ctx, 0, assistant.TurnConceptIndex, turn, meta)
	if err != nil {
		code, _ := runFailure(ctx, "index summary", err)
		return course.SummaryResult{Error: code}
	}
	return course.SummaryResult{Summary: text, Hash: hash}
}

// showIndexConcepts is the concepts of a library course's current index
// (#225), merged, with where each appears.
func (s *Server) showIndexConcepts(w http.ResponseWriter, r *http.Request) {
	c, ok := s.libraryCourse(w, r)
	if !ok {
		return
	}
	concepts, err := s.courses.ConceptsOf(r.Context(), c.ID, c.FilesDir)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"concepts": concepts})
}

// showIndexTree is the summary tree of a library course's current index
// (#225): course, chapters, lessons.
func (s *Server) showIndexTree(w http.ResponseWriter, r *http.Request) {
	c, ok := s.libraryCourse(w, r)
	if !ok {
		return
	}
	t, err := s.courses.SummaryTreeOf(r.Context(), c.ID, c.FilesDir)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// libraryCourse is the library course of the request's {id}; it answers
// course_not_found itself for one that is not.
func (s *Server) libraryCourse(w http.ResponseWriter, r *http.Request) (course.Detail, bool) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return course.Detail{}, false
	}
	c, err := s.courses.Get(r.Context(), currentUser(r), id)
	if errors.Is(err, course.ErrNotFound) || err == nil && c.Visibility != course.Public {
		writeError(w, http.StatusNotFound, "course_not_found", "课程不存在")
		return c, false
	}
	if err != nil {
		writeInternal(w, r, err)
		return c, false
	}
	return c, true
}
