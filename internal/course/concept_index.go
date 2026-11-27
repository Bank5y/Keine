package course

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/jackc/pgx/v5"
)

// A concept index's status (#158, #224): a version starts with none, and
// has one only once an admin builds it. A build extracts its lessons side
// by side; it is built when every lesson was, partial when some failed
// (and still links by the rest), failed when none was.
type ConceptIndexStatus string

const (
	ConceptIndexNone     ConceptIndexStatus = "none"
	ConceptIndexBuilding ConceptIndexStatus = "building"
	ConceptIndexBuilt    ConceptIndexStatus = "built"
	ConceptIndexPartial  ConceptIndexStatus = "partial"
	ConceptIndexFailed   ConceptIndexStatus = "failed"
)

// A lesson's status in a build.
type LessonIndexStatus string

const (
	LessonPending LessonIndexStatus = "pending"
	LessonRunning LessonIndexStatus = "running"
	LessonDone    LessonIndexStatus = "done"
	LessonFailed  LessonIndexStatus = "failed"
)

var (
	// ErrConceptIndexBuilding: the version's index is being built already.
	ErrConceptIndexBuilding = errors.New("concept index building")
	// ErrIndexLessonNotFound: the lesson is not one of the index's.
	ErrIndexLessonNotFound = errors.New("index lesson not found")
)

// Extractor is what extracts an index's lessons: the prompt's version and
// the model (its name at the provider, and as the admin calls it). An index
// extracted by another is stale.
type Extractor struct {
	PromptVersion string
	Model         string
	ModelName     string
}

// ConceptIndexCourse is a library course with the index of its current version,
// as the admin page lists it.
type ConceptIndexCourse struct {
	ID         int64              `json:"id"`
	Title      string             `json:"title"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	Lessons    int                `json:"lessons"`
	Status     ConceptIndexStatus `json:"status"`
	Entries    int                `json:"entries"`
	Failed     int                `json:"failed"`
	Error      string             `json:"error,omitempty"`
	StartedAt  *time.Time         `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt"`
	Extractor  Extractor          `json:"-"`
	// Stale: extracted by another prompt version or model than the current
	// one (StaleFor); it still links until rebuilt.
	Stale bool `json:"stale"`
}

// ConceptIndexes lists the library's courses with the status of their
// current version's index, by title.
func (s *Service) ConceptIndexes(ctx context.Context) ([]ConceptIndexCourse, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.title, c.updated_at,
		       (SELECT count(*) FROM lessons l WHERE l.course_id = c.id AND l.file IS NOT NULL),
		       coalesce(i.status, 'none'),
		       (SELECT count(*) FROM concept_entries e WHERE e.index_id = i.id),
		       (SELECT count(*) FROM concept_index_lessons il WHERE il.index_id = i.id AND il.status = 'failed'),
		       coalesce(i.error, ''), i.started_at, i.finished_at,
		       coalesce(i.prompt_version, ''), coalesce(i.model, ''), coalesce(i.model_name, '')
		FROM courses c
		LEFT JOIN concept_indexes i ON i.course_id = c.id AND i.files_dir = c.files_dir
		WHERE c.visibility = 'public' AND c.deleted_at IS NULL
		ORDER BY c.title, c.id`)
	if err != nil {
		return nil, err
	}
	out := []ConceptIndexCourse{}
	var c ConceptIndexCourse
	_, err = pgx.ForEachRow(rows, []any{&c.ID, &c.Title, &c.UpdatedAt, &c.Lessons, &c.Status, &c.Entries, &c.Failed, &c.Error, &c.StartedAt, &c.FinishedAt,
		&c.Extractor.PromptVersion, &c.Extractor.Model, &c.Extractor.ModelName}, func() error {
		out = append(out, c)
		return nil
	})
	return out, err
}

// StaleFor tells whether an index of this status, extracted by was, is
// stale now that cur extracts: another prompt version, or another model
// when the current one is known. An index not built yet is not stale.
func StaleFor(status ConceptIndexStatus, was, cur Extractor) bool {
	if status != ConceptIndexBuilt && status != ConceptIndexPartial {
		return false
	}
	return was.PromptVersion != cur.PromptVersion || cur.Model != "" && was.Model != cur.Model
}

// ConceptEntry is an entry of a concept index: a concept of the course
// text, the lesson and section it is in, and a summary.
type ConceptEntry struct {
	Name    string      `json:"name"`
	Aliases []string    `json:"aliases"`
	Lesson  EntryLesson `json:"lesson"`
	// Section is nil when the concept is at the lesson's top.
	Section *EntrySection `json:"section"`
	Summary string        `json:"summary"`
	// Role is "define" where the lesson explains the concept, "use" where
	// it uses one explained elsewhere.
	Role string `json:"role"`
	// revision is what revised it for recall, and weight its weight there
	revision *Revision
	weight   float64
}

// EntryLesson is the lesson an entry is in.
type EntryLesson struct {
	ID    int64  `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// EntrySection is a section of a lesson: its anchor in the reader and its
// heading.
type EntrySection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// IndexLesson is a lesson of an index's build, as the admin page follows
// it: where it stands, why it failed, whether its entries were kept from
// an earlier extraction, and how many it has.
type IndexLesson struct {
	Key     string            `json:"key"`
	Title   string            `json:"title"`
	Chapter string            `json:"chapter"`
	Status  LessonIndexStatus `json:"status"`
	Error   string            `json:"error,omitempty"`
	Reused  bool              `json:"reused"`
	Entries int               `json:"entries"`
	Summary string            `json:"summary"`
}

// IndexProgress counts an index's lessons by where they stand.
type IndexProgress struct {
	Total  int `json:"total"`
	Done   int `json:"done"`
	Failed int `json:"failed"`
}

// ConceptIndex is the index of one version of a course.
type ConceptIndex struct {
	Status ConceptIndexStatus `json:"status"`
	// Entries are those of the lessons extracted so far, in course order:
	// an index links by them while it is partial, stale, or rebuilt.
	Entries    []ConceptEntry `json:"entries"`
	Lessons    []IndexLesson  `json:"lessons"`
	Progress   IndexProgress  `json:"progress"`
	Extractor  Extractor      `json:"-"`
	StartedAt  *time.Time     `json:"startedAt"`
	FinishedAt *time.Time     `json:"finishedAt"`
	// Stale and PromptVersion and ModelName are for the admin page; the
	// server sets Stale (StaleFor).
	Stale         bool   `json:"stale"`
	PromptVersion string `json:"promptVersion"`
	ModelName     string `json:"modelName"`
	// Stage is where a build is: extracting its lessons, or summarizing
	// the course once they are through (#225).
	Stage string `json:"stage"`
	// Concepts are the entries merged (#225), which recall uses once there
	// are some; none before the first build that merged them.
	Concepts []IndexConcept `json:"-"`
	// Embedding is how far the version's passages are embedded, while
	// embedding is on (#227); the server sets it.
	Embedding *EmbeddingProgress `json:"embedding,omitempty"`
}

// ConceptIndexOf is the index of a version of a course (its files folder,
// Summary.FilesDir): its status, its lessons, and the entries extracted so
// far in course order. It does not check who may read the course.
func (s *Service) ConceptIndexOf(ctx context.Context, courseID int64, filesDir string) (ConceptIndex, error) {
	x := ConceptIndex{Status: ConceptIndexNone, Entries: []ConceptEntry{}, Lessons: []IndexLesson{}}
	var indexID int64
	err := s.db.QueryRow(ctx, `
		SELECT id, status, stage, prompt_version, model, model_name, started_at, finished_at
		FROM concept_indexes WHERE course_id = $1 AND files_dir = $2`, courseID, filesDir).
		Scan(&indexID, &x.Status, &x.Stage, &x.Extractor.PromptVersion, &x.Extractor.Model, &x.Extractor.ModelName, &x.StartedAt, &x.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, nil
	}
	if err != nil {
		return x, err
	}
	x.PromptVersion, x.ModelName = x.Extractor.PromptVersion, x.Extractor.ModelName
	rows, err := s.db.Query(ctx, `
		SELECT il.lesson_key, coalesce(l.title, il.lesson_key), coalesce(ch.title, ''), il.status, coalesce(il.error, ''), il.reused, il.summary,
		       (SELECT count(*) FROM concept_entries e WHERE e.index_id = il.index_id AND e.lesson_key = il.lesson_key)
		FROM concept_index_lessons il
		LEFT JOIN lessons l ON l.course_id = $2 AND l.key = il.lesson_key
		LEFT JOIN chapters ch ON ch.id = l.chapter_id
		WHERE il.index_id = $1
		ORDER BY ch.position NULLS LAST, l.position NULLS LAST, il.lesson_key`, indexID, courseID)
	if err != nil {
		return x, err
	}
	var il IndexLesson
	if _, err := pgx.ForEachRow(rows, []any{&il.Key, &il.Title, &il.Chapter, &il.Status, &il.Error, &il.Reused, &il.Summary, &il.Entries}, func() error {
		x.Lessons = append(x.Lessons, il)
		x.Progress.Total++
		switch il.Status {
		case LessonDone:
			x.Progress.Done++
		case LessonFailed:
			x.Progress.Failed++
		}
		return nil
	}); err != nil {
		return x, err
	}
	rows, err = s.db.Query(ctx, `
		SELECT e.name, e.aliases, l.id, l.key, l.title, e.section_id, e.section_title, e.summary, e.role
		FROM concept_entries e
		JOIN lessons l ON l.course_id = $2 AND l.key = e.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE e.index_id = $1
		ORDER BY ch.position, l.position, e.position`, indexID, courseID)
	if err != nil {
		return x, err
	}
	var e ConceptEntry
	var sectionID, sectionTitle string
	_, err = pgx.ForEachRow(rows, []any{&e.Name, &e.Aliases, &e.Lesson.ID, &e.Lesson.Key, &e.Lesson.Title, &sectionID, &sectionTitle, &e.Summary, &e.Role}, func() error {
		entry := e
		entry.Aliases = append([]string{}, e.Aliases...)
		entry.Section = nil
		if sectionID != "" {
			entry.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		x.Entries = append(x.Entries, entry)
		return nil
	})
	if err != nil {
		return x, err
	}
	x.Concepts, err = s.ConceptsOf(ctx, courseID, filesDir)
	return x, err
}

// ConceptIndexBuild is a build of a version's index under way: the lessons
// it has to extract (those an earlier extraction of the same text, prompt
// and model could not stand for) and what extracts them.
type ConceptIndexBuild struct {
	ID          int64
	CourseID    int64
	CourseTitle string
	Extractor   Extractor
	Lessons     []ConceptIndexLesson
}

// ConceptIndexLesson is a lesson of the version to extract its concepts from.
type ConceptIndexLesson struct {
	Key, Title, Chapter string
	Markdown            string
	hash                string
}

// ExtractedConcept is a concept as the extraction gives it: its section by
// anchor and heading ("" for the lesson's top), and whether the lesson
// defines it ("define") or uses it ("use").
type ExtractedConcept struct {
	Name         string
	Aliases      []string
	SectionID    string
	SectionTitle string
	Summary      string
	Role         string
}

// lessonFile is a lesson of a course's current version with its text.
type lessonFile struct {
	ConceptIndexLesson
	file string
}

// currentLessons reads the texts of a library course's current version,
// with no lock held: its title, folder and lessons with a text, in course
// order. A lesson whose text is missing from storage is logged and left
// out.
func (s *Service) currentLessons(ctx context.Context, courseID int64) (title, dir string, files []lessonFile, err error) {
	err = s.db.QueryRow(ctx, "SELECT title, files_dir FROM courses WHERE id = $1 AND visibility = 'public' AND deleted_at IS NULL", courseID).
		Scan(&title, &dir)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, ErrNotFound
	}
	if err != nil {
		return "", "", nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.key, l.title, ch.title, l.file FROM lessons l JOIN chapters ch ON ch.id = l.chapter_id
		WHERE l.course_id = $1 AND l.file IS NOT NULL ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return "", "", nil, err
	}
	var lf lessonFile
	var listed []lessonFile
	if _, err := pgx.ForEachRow(rows, []any{&lf.Key, &lf.Title, &lf.Chapter, &lf.file}, func() error {
		listed = append(listed, lf)
		return nil
	}); err != nil {
		return "", "", nil, err
	}
	for _, f := range listed {
		t, err := s.readText(ctx, courseID, dir, LessonText{File: f.file})
		if errors.Is(err, storage.ErrNotFound) {
			slog.Warn("lesson text missing", "course", courseID, "file", filesPrefix(courseID, dir)+f.file)
			continue
		}
		if err != nil {
			return "", "", nil, err
		}
		f.Markdown = t.Markdown
		sum := sha256.Sum256([]byte(t.Markdown))
		f.hash = hex.EncodeToString(sum[:])
		files = append(files, f)
	}
	return title, dir, files, nil
}

// lockIndex checks, under the lock, that the course still shows version
// dir, and locks its index row, making one when it has none; a build under
// way already is ErrConceptIndexBuilding. It tells whether the row is new.
func lockIndex(ctx context.Context, tx pgx.Tx, courseID int64, dir string, now time.Time, ex Extractor) (int64, error) {
	var cur string
	err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 AND visibility = 'public' AND deleted_at IS NULL FOR SHARE", courseID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if cur != dir {
		return 0, ErrCourseBusy
	}
	var id int64
	var status ConceptIndexStatus
	err = tx.QueryRow(ctx, "SELECT id, status FROM concept_indexes WHERE course_id = $1 AND files_dir = $2 FOR UPDATE", courseID, dir).Scan(&id, &status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		err = tx.QueryRow(ctx, `INSERT INTO concept_indexes (course_id, files_dir, status, started_at, prompt_version, model, model_name)
			VALUES ($1, $2, 'building', $3, $4, $5, $6) ON CONFLICT (course_id, files_dir) DO NOTHING RETURNING id`,
			courseID, dir, now, ex.PromptVersion, ex.Model, ex.ModelName).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrConceptIndexBuilding // another admin started it just now
		}
		return id, err
	case err != nil:
		return 0, err
	case status == ConceptIndexBuilding:
		return 0, ErrConceptIndexBuilding
	}
	return id, nil
}

// StartConceptIndex starts building the index of a library course's
// current version, from none or again (after it failed, was partial, or
// went stale), extracted by ex. A lesson whose text an earlier extraction
// of the course read with the same prompt version and model (a previous
// version, or this one) keeps those entries; the rest are returned to be
// extracted, and kept with KeepLessonConcepts or FailLessonConcepts. A
// lesson whose text is missing from storage is logged and left out.
//
// The texts are read with no lock held; the course is checked again under
// the lock, and the start begins over if it switched version meanwhile.
func (s *Service) StartConceptIndex(ctx context.Context, courseID int64, ex Extractor) (ConceptIndexBuild, error) {
	for attempt := 1; ; attempt++ {
		b, err := s.startConceptIndex(ctx, courseID, ex)
		if !errors.Is(err, ErrCourseBusy) || attempt == busyAttempts {
			return b, err
		}
	}
}

func (s *Service) startConceptIndex(ctx context.Context, courseID int64, ex Extractor) (ConceptIndexBuild, error) {
	b := ConceptIndexBuild{CourseID: courseID, Extractor: ex}
	title, dir, files, err := s.currentLessons(ctx, courseID)
	if err != nil {
		return b, err
	}
	b.CourseTitle = title
	err = s.tx(ctx, func(tx pgx.Tx) error {
		b.Lessons = nil
		id, err := lockIndex(ctx, tx, courseID, dir, s.now(), ex)
		if err != nil {
			return err
		}
		b.ID = id
		if _, err := tx.Exec(ctx, `UPDATE concept_indexes SET status = 'building', stage = 'extract', error = NULL, started_at = $2, finished_at = NULL,
			prompt_version = $3, model = $4, model_name = $5 WHERE id = $1`, id, s.now(), ex.PromptVersion, ex.Model, ex.ModelName); err != nil {
			return err
		}
		// lessons gone from this version, or whose text is missing
		keys := make([]string, len(files))
		for i, f := range files {
			keys[i] = f.Key
		}
		if _, err := tx.Exec(ctx, "DELETE FROM concept_index_lessons WHERE index_id = $1 AND lesson_key <> ALL($2)", id, keys); err != nil {
			return err
		}
		for _, f := range files {
			kept, err := reuseLessonConcepts(ctx, tx, id, courseID, f.Key, f.hash, ex)
			if err != nil {
				return err
			}
			if !kept {
				b.Lessons = append(b.Lessons, f.ConceptIndexLesson)
			}
		}
		return nil
	})
	return b, err
}

// reuseLessonConcepts makes sure the index has the lesson's entries from
// an extraction of text of the given hash by ex: kept already, or copied
// from another index of the course that has one. It tells whether it has
// them; when not, the lesson is left pending, its old entries gone.
func reuseLessonConcepts(ctx context.Context, tx pgx.Tx, indexID, courseID int64, key, hash string, ex Extractor) (bool, error) {
	var own, status, prompt, model string
	err := tx.QueryRow(ctx, "SELECT text_hash, status, prompt_version, model FROM concept_index_lessons WHERE index_id = $1 AND lesson_key = $2", indexID, key).
		Scan(&own, &status, &prompt, &model)
	switch {
	case err == nil && own == hash && status == string(LessonDone) && prompt == ex.PromptVersion && model == ex.Model:
		_, err = tx.Exec(ctx, "UPDATE concept_index_lessons SET reused = true WHERE index_id = $1 AND lesson_key = $2", indexID, key)
		return true, err
	case err == nil:
		// the text, the prompt or the model changed since, or it failed:
		// extracted again
		if _, err := tx.Exec(ctx, "DELETE FROM concept_index_lessons WHERE index_id = $1 AND lesson_key = $2", indexID, key); err != nil {
			return false, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}
	var from int64
	var summary string
	err = tx.QueryRow(ctx, `
		SELECT il.index_id, il.summary FROM concept_index_lessons il JOIN concept_indexes i ON i.id = il.index_id
		WHERE i.course_id = $1 AND il.index_id <> $2 AND il.lesson_key = $3 AND il.text_hash = $4
		  AND il.status = 'done' AND il.prompt_version = $5 AND il.model = $6
		ORDER BY i.started_at DESC LIMIT 1`, courseID, indexID, key, hash, ex.PromptVersion, ex.Model).Scan(&from, &summary)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO concept_index_lessons (index_id, lesson_key, text_hash, status, prompt_version, model)
			VALUES ($1, $2, $3, 'pending', $4, $5)`, indexID, key, hash, ex.PromptVersion, ex.Model)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO concept_index_lessons (index_id, lesson_key, text_hash, status, reused, prompt_version, model, summary)
		VALUES ($1, $2, $3, 'done', true, $4, $5, $6)`, indexID, key, hash, ex.PromptVersion, ex.Model, summary); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary, role)
		SELECT $1, lesson_key, position, name, aliases, section_id, section_title, summary, role
		FROM concept_entries WHERE index_id = $2 AND lesson_key = $3`, indexID, from, key)
	return err == nil, err
}

// RetryConceptLesson starts extracting one lesson of the current
// version's index again, by ex (#224): the build has that lesson only,
// whatever its status was. The index must exist and not be building.
func (s *Service) RetryConceptLesson(ctx context.Context, courseID int64, key string, ex Extractor) (ConceptIndexBuild, error) {
	for attempt := 1; ; attempt++ {
		b, err := s.retryConceptLesson(ctx, courseID, key, ex)
		if !errors.Is(err, ErrCourseBusy) || attempt == busyAttempts {
			return b, err
		}
	}
}

func (s *Service) retryConceptLesson(ctx context.Context, courseID int64, key string, ex Extractor) (ConceptIndexBuild, error) {
	b := ConceptIndexBuild{CourseID: courseID, Extractor: ex}
	title, dir, files, err := s.currentLessons(ctx, courseID)
	if err != nil {
		return b, err
	}
	b.CourseTitle = title
	var lesson *lessonFile
	for i := range files {
		if files[i].Key == key {
			lesson = &files[i]
		}
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM concept_indexes WHERE course_id = $1 AND files_dir = $2)", courseID, dir).Scan(&exists); err != nil {
			return err
		}
		if !exists || lesson == nil {
			return ErrIndexLessonNotFound
		}
		id, err := lockIndex(ctx, tx, courseID, dir, s.now(), ex)
		if err != nil {
			return err
		}
		b.ID = id
		tag, err := tx.Exec(ctx, `UPDATE concept_index_lessons SET status = 'pending', error = NULL, reused = false, text_hash = $3, prompt_version = $4, model = $5
			WHERE index_id = $1 AND lesson_key = $2`, id, key, lesson.hash, ex.PromptVersion, ex.Model)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrIndexLessonNotFound
		}
		if _, err := tx.Exec(ctx, "DELETE FROM concept_entries WHERE index_id = $1 AND lesson_key = $2", id, key); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE concept_indexes SET status = 'building', stage = 'extract', error = NULL, finished_at = NULL WHERE id = $1", id)
		b.Lessons = []ConceptIndexLesson{lesson.ConceptIndexLesson}
		return err
	})
	return b, err
}

// RunLessonConcepts marks a lesson of the build as being extracted.
func (s *Service) RunLessonConcepts(ctx context.Context, b ConceptIndexBuild, l ConceptIndexLesson) error {
	_, err := s.db.Exec(ctx, "UPDATE concept_index_lessons SET status = 'running' WHERE index_id = $1 AND lesson_key = $2", b.ID, l.Key)
	return err
}

// KeepLessonConcepts keeps what was extracted of a lesson of the build:
// its summary and concepts.
func (s *Service) KeepLessonConcepts(ctx context.Context, b ConceptIndexBuild, l ConceptIndexLesson, summary string, concepts []ExtractedConcept) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE concept_index_lessons SET status = 'done', error = NULL, summary = $3 WHERE index_id = $1 AND lesson_key = $2", b.ID, l.Key, summary); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM concept_entries WHERE index_id = $1 AND lesson_key = $2", b.ID, l.Key); err != nil {
			return err
		}
		for i, c := range concepts {
			aliases := c.Aliases
			if aliases == nil {
				aliases = []string{}
			}
			role := c.Role
			if role != "use" {
				role = "define"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary, role)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, b.ID, l.Key, i, c.Name, aliases, c.SectionID, c.SectionTitle, c.Summary, role); err != nil {
				return err
			}
		}
		return nil
	})
}

// FailLessonConcepts records that a lesson of the build failed, with the
// run error code it failed with; the other lessons go on.
func (s *Service) FailLessonConcepts(ctx context.Context, b ConceptIndexBuild, l ConceptIndexLesson, code string) error {
	_, err := s.db.Exec(ctx, "UPDATE concept_index_lessons SET status = 'failed', error = $3 WHERE index_id = $1 AND lesson_key = $2", b.ID, l.Key, code)
	return err
}

// FinishConceptIndex ends a build once its lessons are through: built
// when every lesson was extracted, partial when some failed, failed when
// none was. A lesson the build never reached is failed as interrupted.
func (s *Service) FinishConceptIndex(ctx context.Context, b ConceptIndexBuild) error {
	return s.finishIndexes(ctx, &b.ID)
}

// finishIndexes ends the build of index id, or every build under way when
// id is nil: lessons left pending or running fail as interrupted, then
// each index takes its status from its lessons.
func (s *Service) finishIndexes(ctx context.Context, id *int64) error {
	if _, err := s.db.Exec(ctx, `
		UPDATE concept_index_lessons SET status = 'failed', error = 'interrupted'
		WHERE status IN ('pending', 'running')
		  AND index_id IN (SELECT id FROM concept_indexes WHERE status = 'building' AND ($1::bigint IS NULL OR id = $1))`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `
		UPDATE concept_indexes i SET finished_at = $1, status = CASE
			WHEN NOT EXISTS (SELECT 1 FROM concept_index_lessons il WHERE il.index_id = i.id AND il.status = 'failed') THEN 'built'
			WHEN EXISTS (SELECT 1 FROM concept_index_lessons il WHERE il.index_id = i.id AND il.status = 'done') THEN 'partial'
			ELSE 'failed' END
		WHERE status = 'building' AND ($2::bigint IS NULL OR id = $2)`, s.now(), id)
	return err
}

// FailInterruptedConceptIndexes ends the builds a stopped server left
// behind: the lessons it never got to fail as interrupted, and can be
// tried again.
func (s *Service) FailInterruptedConceptIndexes(ctx context.Context) error {
	return s.finishIndexes(ctx, nil)
}
