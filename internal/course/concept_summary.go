package course

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// MaxChapterLessons is how many lessons a chapter summary covers well: a
// longer chapter is marked too long on the admin page, not split (#225).
const MaxChapterLessons = 15

// SummaryLesson is a lesson in the summary tree: its key, title, status in
// the index and summary.
type SummaryLesson struct {
	Key     string            `json:"key"`
	Title   string            `json:"title"`
	Status  LessonIndexStatus `json:"status"`
	Summary string            `json:"summary"`
}

// SummaryChapter is a chapter in the summary tree, with its summary and
// the hash of what it was written from.
type SummaryChapter struct {
	Key     string          `json:"key"`
	Title   string          `json:"title"`
	Summary string          `json:"summary"`
	Error   string          `json:"error,omitempty"`
	TooLong bool            `json:"tooLong"`
	Lessons []SummaryLesson `json:"lessons"`
	Hash    string          `json:"-"`
}

// SummaryTree is an index's summary tree (#225): course → chapter → lesson.
type SummaryTree struct {
	Summary  string           `json:"summary"`
	Error    string           `json:"error,omitempty"`
	Chapters []SummaryChapter `json:"chapters"`
	Hash     string           `json:"-"`
}

// SummaryTreeOf is the summary tree of a version's index, with the
// chapters of the course that have a lesson in the index, in course order.
func (s *Service) SummaryTreeOf(ctx context.Context, courseID int64, filesDir string) (SummaryTree, error) {
	var id int64
	err := s.db.QueryRow(ctx, "SELECT id FROM concept_indexes WHERE course_id = $1 AND files_dir = $2", courseID, filesDir).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return SummaryTree{Chapters: []SummaryChapter{}}, nil
	}
	if err != nil {
		return SummaryTree{}, err
	}
	return s.summaryTree(ctx, id, courseID)
}

func (s *Service) summaryTree(ctx context.Context, indexID, courseID int64) (SummaryTree, error) {
	t := SummaryTree{Chapters: []SummaryChapter{}}
	var errCode *string
	if err := s.db.QueryRow(ctx, "SELECT summary, summary_hash, summary_error FROM concept_indexes WHERE id = $1", indexID).Scan(&t.Summary, &t.Hash, &errCode); err != nil {
		return t, err
	}
	if errCode != nil {
		t.Error = *errCode
	}
	rows, err := s.db.Query(ctx, `
		SELECT ch.key, ch.title, coalesce(ic.summary, ''), coalesce(ic.summary_hash, ''), coalesce(ic.error, ''), coalesce(ic.too_long, false),
		       l.key, l.title, il.status, il.summary
		FROM concept_index_lessons il
		JOIN lessons l ON l.course_id = $2 AND l.key = il.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		LEFT JOIN index_chapters ic ON ic.index_id = il.index_id AND ic.chapter_key = ch.key
		WHERE il.index_id = $1
		ORDER BY ch.position, l.position`, indexID, courseID)
	if err != nil {
		return t, err
	}
	var c SummaryChapter
	var l SummaryLesson
	_, err = pgx.ForEachRow(rows, []any{&c.Key, &c.Title, &c.Summary, &c.Hash, &c.Error, &c.TooLong, &l.Key, &l.Title, &l.Status, &l.Summary}, func() error {
		if len(t.Chapters) == 0 || t.Chapters[len(t.Chapters)-1].Key != c.Key {
			cc := c
			cc.Lessons = nil
			t.Chapters = append(t.Chapters, cc)
		}
		last := &t.Chapters[len(t.Chapters)-1]
		last.Lessons = append(last.Lessons, l)
		return nil
	})
	return t, err
}

// SummaryPlan is the summary tree of a build's index as it stands, for
// the build to write what changed.
func (s *Service) SummaryPlan(ctx context.Context, b ConceptIndexBuild) (SummaryTree, error) {
	return s.summaryTree(ctx, b.ID, b.CourseID)
}

// SummarizingConceptIndex moves a build on to its summary stage.
func (s *Service) SummarizingConceptIndex(ctx context.Context, b ConceptIndexBuild) error {
	_, err := s.db.Exec(ctx, "UPDATE concept_indexes SET stage = 'summarize' WHERE id = $1", b.ID)
	return err
}

// SummaryResult is how writing one summary went: the summary and the hash
// of what it was written from, or the run error code it failed with.
type SummaryResult struct {
	Summary, Hash, Error string
	TooLong              bool
}

// KeepChapterSummary keeps a chapter's summary in a build's index.
func (s *Service) KeepChapterSummary(ctx context.Context, b ConceptIndexBuild, chapterKey string, r SummaryResult) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO index_chapters (index_id, chapter_key, summary, summary_hash, error, too_long) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		ON CONFLICT (index_id, chapter_key) DO UPDATE SET summary = $3, summary_hash = $4, error = NULLIF($5, ''), too_long = $6`,
		b.ID, chapterKey, r.Summary, r.Hash, r.Error, r.TooLong)
	return err
}

// KeepCourseSummary keeps the course's summary in a build's index.
func (s *Service) KeepCourseSummary(ctx context.Context, b ConceptIndexBuild, r SummaryResult) error {
	_, err := s.db.Exec(ctx, "UPDATE concept_indexes SET summary = $2, summary_hash = $3, summary_error = NULLIF($4, '') WHERE id = $1",
		b.ID, r.Summary, r.Hash, r.Error)
	return err
}
