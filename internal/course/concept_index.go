package course

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// A concept index's status (#158): a version starts with none, and has one
// only once an admin builds it.
type ConceptIndexStatus string

const (
	ConceptIndexNone     ConceptIndexStatus = "none"
	ConceptIndexBuilding ConceptIndexStatus = "building"
	ConceptIndexBuilt    ConceptIndexStatus = "built"
	ConceptIndexFailed   ConceptIndexStatus = "failed"
)

var (
	// ErrConceptIndexBuilding: the version's index is being built already.
	ErrConceptIndexBuilding = errors.New("concept index building")
	// ErrConceptIndexBuilt: the version's index is built; a new version gets its own.
	ErrConceptIndexBuilt = errors.New("concept index built")
)

// ConceptIndexCourse is a library course with the index of its current version,
// as the admin page lists it.
type ConceptIndexCourse struct {
	ID         int64              `json:"id"`
	Title      string             `json:"title"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	Lessons    int                `json:"lessons"`
	Status     ConceptIndexStatus `json:"status"`
	Entries    int                `json:"entries"`
	Error      string             `json:"error,omitempty"`
	StartedAt  *time.Time         `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt"`
}

// ConceptIndexes lists the library's courses with the status of their
// current version's index, by title.
func (s *Service) ConceptIndexes(ctx context.Context) ([]ConceptIndexCourse, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.title, c.updated_at,
		       (SELECT count(*) FROM lessons l WHERE l.course_id = c.id AND l.file IS NOT NULL),
		       coalesce(i.status, 'none'),
		       (SELECT count(*) FROM concept_entries e WHERE e.index_id = i.id),
		       coalesce(i.error, ''), i.started_at, i.finished_at
		FROM courses c
		LEFT JOIN concept_indexes i ON i.course_id = c.id AND i.files_dir = c.files_dir
		WHERE c.visibility = 'public' AND c.deleted_at IS NULL
		ORDER BY c.title, c.id`)
	if err != nil {
		return nil, err
	}
	out := []ConceptIndexCourse{}
	var c ConceptIndexCourse
	_, err = pgx.ForEachRow(rows, []any{&c.ID, &c.Title, &c.UpdatedAt, &c.Lessons, &c.Status, &c.Entries, &c.Error, &c.StartedAt, &c.FinishedAt}, func() error {
		out = append(out, c)
		return nil
	})
	return out, err
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

// ConceptIndex is the index of one version of a course.
type ConceptIndex struct {
	Status  ConceptIndexStatus `json:"status"`
	Entries []ConceptEntry     `json:"entries"`
}

// Find is the entries whose name or an alias is term, compared
// normalized (NormalizeConcept).
func (x ConceptIndex) Find(term string) []ConceptEntry {
	want := NormalizeConcept(term)
	var out []ConceptEntry
	for _, e := range x.Entries {
		for _, n := range append([]string{e.Name}, e.Aliases...) {
			if NormalizeConcept(n) == want {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// minMentionRunes is the shortest a name or an alias can be, normalized,
// and still be looked for in a text: a single character matches anywhere.
const minMentionRunes = 2

// Mentioned is the entries a text names, by name or alias, compared
// normalized (NormalizeConcept), in the order the text first names them,
// at most max of them (ADR 0011). Entries of the lesson keyed exceptLesson
// are left out, but still hide the shorter names they overlap.
//
// A name's letters and digits only match a whole word: "4C" is not named
// in "G4C" (#177). Where two entries' names overlap in the text, only the
// longer name is named there: "日志复制" names no "日志"; of names as long
// as each other, both are.
func (x ConceptIndex) Mentioned(text, exceptLesson string, max int) []ConceptEntry {
	t, breaks := normalizeWithBreaks(text)
	type span struct{ entry, start, end, runes int }
	var spans []span
	for ei, e := range x.Entries {
		for _, n := range append([]string{e.Name}, e.Aliases...) {
			n = NormalizeConcept(n)
			runes := utf8.RuneCountInString(n)
			if runes < minMentionRunes {
				continue
			}
			for from := 0; from < len(t); {
				i := strings.Index(t[from:], n)
				if i < 0 {
					break
				}
				i += from
				if j := i + len(n); wordStart(t, breaks, i, n) && wordEnd(t, breaks, j, n) {
					spans = append(spans, span{ei, i, j, runes})
				}
				_, size := utf8.DecodeRuneInString(t[i:])
				from = i + size
			}
		}
	}
	first := map[int]int{}
	for _, s := range spans {
		shadowed := slices.ContainsFunc(spans, func(o span) bool {
			return o.entry != s.entry && o.runes > s.runes && o.start < s.end && s.start < o.end
		})
		if x.Entries[s.entry].Lesson.Key == exceptLesson {
			continue
		}
		if at, ok := first[s.entry]; !shadowed && (!ok || s.start < at) {
			first[s.entry] = s.start
		}
	}
	var named []int
	for ei := range first {
		named = append(named, ei)
	}
	slices.SortFunc(named, func(a, b int) int {
		if first[a] != first[b] {
			return first[a] - first[b]
		}
		return a - b
	})
	var out []ConceptEntry
	for _, ei := range named[:min(len(named), max)] {
		out = append(out, x.Entries[ei])
	}
	return out
}

// normalizeWithBreaks is NormalizeConcept(s), and the places in it where
// white space was taken out: a word ends there.
func normalizeWithBreaks(s string) (string, map[int]bool) {
	var b strings.Builder
	breaks := map[int]bool{}
	for i, f := range strings.Fields(strings.ToLower(norm.NFKC.String(s))) {
		if i > 0 {
			breaks[b.Len()] = true
		}
		b.WriteString(f)
	}
	return b.String(), breaks
}

// wordRune is a letter or digit of a word that white space or other
// characters end: not one of the scripts written without spaces.
func wordRune(r rune) bool {
	if unicode.IsDigit(r) {
		return true
	}
	return unicode.IsLetter(r) && !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// wordStart is whether name n found at i in t starts a word there, when it
// starts with a letter or digit.
func wordStart(t string, breaks map[int]bool, i int, n string) bool {
	if r, _ := utf8.DecodeRuneInString(n); !wordRune(r) || i == 0 || breaks[i] {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(t[:i])
	return !wordRune(r)
}

// wordEnd is whether name n found ending at j in t ends a word there, when
// it ends with a letter or digit.
func wordEnd(t string, breaks map[int]bool, j int, n string) bool {
	if r, _ := utf8.DecodeLastRuneInString(n); !wordRune(r) || j == len(t) || breaks[j] {
		return true
	}
	r, _ := utf8.DecodeRuneInString(t[j:])
	return !wordRune(r)
}

// NormalizeConcept is how concept names compare: width and compatibility
// forms folded (NFKC), lower case, white space taken out.
func NormalizeConcept(s string) string {
	t, _ := normalizeWithBreaks(s)
	return t
}

// ConceptIndexOf is the index of a version of a course (its files folder,
// Summary.FilesDir): its status, and its entries in course order once it is
// built. It does not check who may read the course.
func (s *Service) ConceptIndexOf(ctx context.Context, courseID int64, filesDir string) (ConceptIndex, error) {
	x := ConceptIndex{Status: ConceptIndexNone, Entries: []ConceptEntry{}}
	var indexID int64
	err := s.db.QueryRow(ctx, "SELECT id, status FROM concept_indexes WHERE course_id = $1 AND files_dir = $2", courseID, filesDir).
		Scan(&indexID, &x.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, nil
	}
	if err != nil || x.Status != ConceptIndexBuilt {
		return x, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT e.name, e.aliases, l.id, l.key, l.title, e.section_id, e.section_title, e.summary
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
	_, err = pgx.ForEachRow(rows, []any{&e.Name, &e.Aliases, &e.Lesson.ID, &e.Lesson.Key, &e.Lesson.Title, &sectionID, &sectionTitle, &e.Summary}, func() error {
		entry := e
		entry.Aliases = append([]string{}, e.Aliases...)
		if sectionID != "" {
			entry.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		x.Entries = append(x.Entries, entry)
		return nil
	})
	return x, err
}

// ConceptIndexBuild is a build of a version's index under way: the lessons it has
// to extract, those whose text an earlier build extracted already copied.
type ConceptIndexBuild struct {
	ID          int64
	CourseID    int64
	CourseTitle string
	Lessons     []ConceptIndexLesson
}

// ConceptIndexLesson is a lesson of the version to extract its concepts from.
type ConceptIndexLesson struct {
	Key, Title, Chapter string
	Markdown            string
	hash                string
}

// ExtractedConcept is a concept as the extraction gives it: its section by
// anchor and heading ("" for the lesson's top).
type ExtractedConcept struct {
	Name         string
	Aliases      []string
	SectionID    string
	SectionTitle string
	Summary      string
}

// StartConceptIndex starts building the index of a library course's
// current version: from none, or again after it failed. Lessons whose
// text an earlier build of the course extracted (a previous version, or
// this one before it failed) get those entries copied; the rest are
// returned to be extracted, and kept with KeepLessonConcepts. A lesson
// whose text is missing from storage is logged and left out of the index.
//
// The texts are read with no lock held; the course is checked again under
// the lock, and the start begins over if it switched version meanwhile.
func (s *Service) StartConceptIndex(ctx context.Context, courseID int64) (ConceptIndexBuild, error) {
	for attempt := 1; ; attempt++ {
		b, err := s.startConceptIndex(ctx, courseID)
		if !errors.Is(err, ErrCourseBusy) || attempt == busyAttempts {
			return b, err
		}
	}
}

func (s *Service) startConceptIndex(ctx context.Context, courseID int64) (ConceptIndexBuild, error) {
	b := ConceptIndexBuild{CourseID: courseID}
	var dir string
	err := s.db.QueryRow(ctx, "SELECT title, files_dir FROM courses WHERE id = $1 AND visibility = 'public' AND deleted_at IS NULL", courseID).
		Scan(&b.CourseTitle, &dir)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	type lessonFile struct {
		ConceptIndexLesson
		file string
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.key, l.title, ch.title, l.file FROM lessons l JOIN chapters ch ON ch.id = l.chapter_id
		WHERE l.course_id = $1 AND l.file IS NOT NULL ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return b, err
	}
	var lf lessonFile
	var listed []lessonFile
	if _, err := pgx.ForEachRow(rows, []any{&lf.Key, &lf.Title, &lf.Chapter, &lf.file}, func() error {
		listed = append(listed, lf)
		return nil
	}); err != nil {
		return b, err
	}
	var files []lessonFile
	for _, f := range listed {
		t, err := s.readText(ctx, courseID, dir, LessonText{File: f.file})
		if errors.Is(err, storage.ErrNotFound) {
			slog.Warn("lesson text missing", "course", courseID, "file", filesPrefix(courseID, dir)+f.file)
			continue
		}
		if err != nil {
			return b, err
		}
		f.Markdown = t.Markdown
		sum := sha256.Sum256([]byte(t.Markdown))
		f.hash = hex.EncodeToString(sum[:])
		files = append(files, f)
	}

	err = s.tx(ctx, func(tx pgx.Tx) error {
		// the texts were read from dir: under the lock it must still be the
		// version the course shows
		var now string
		err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 AND visibility = 'public' AND deleted_at IS NULL FOR SHARE", courseID).Scan(&now)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if now != dir {
			return ErrCourseBusy
		}
		var status ConceptIndexStatus
		err = tx.QueryRow(ctx, "SELECT id, status FROM concept_indexes WHERE course_id = $1 AND files_dir = $2 FOR UPDATE", courseID, dir).Scan(&b.ID, &status)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = tx.QueryRow(ctx, `INSERT INTO concept_indexes (course_id, files_dir, status, started_at) VALUES ($1, $2, 'building', $3)
				ON CONFLICT (course_id, files_dir) DO NOTHING RETURNING id`, courseID, dir, s.now()).Scan(&b.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrConceptIndexBuilding // another admin started it just now
			}
		case err != nil:
		case status == ConceptIndexBuilding:
			return ErrConceptIndexBuilding
		case status == ConceptIndexBuilt:
			return ErrConceptIndexBuilt
		default:
			_, err = tx.Exec(ctx, "UPDATE concept_indexes SET status = 'building', error = NULL, started_at = $2, finished_at = NULL WHERE id = $1", b.ID, s.now())
		}
		if err != nil {
			return err
		}
		// lessons gone from this version, or whose text is missing, from a
		// build before it failed
		keys := make([]string, len(files))
		for i, f := range files {
			keys[i] = f.Key
		}
		if _, err := tx.Exec(ctx, "DELETE FROM concept_index_lessons WHERE index_id = $1 AND lesson_key <> ALL($2)", b.ID, keys); err != nil {
			return err
		}
		for _, f := range files {
			done, err := copyLessonConcepts(ctx, tx, b.ID, courseID, f.Key, f.hash)
			if err != nil {
				return err
			}
			if !done {
				b.Lessons = append(b.Lessons, f.ConceptIndexLesson)
			}
		}
		return nil
	})
	return b, err
}

// copyLessonConcepts makes sure the index has the lesson's entries from
// text of the given hash: kept already, or copied from another index of
// the course that extracted the same text. It tells whether it has them.
func copyLessonConcepts(ctx context.Context, tx pgx.Tx, indexID, courseID int64, key, hash string) (bool, error) {
	var own string
	err := tx.QueryRow(ctx, "SELECT text_hash FROM concept_index_lessons WHERE index_id = $1 AND lesson_key = $2", indexID, key).Scan(&own)
	switch {
	case err == nil && own == hash:
		return true, nil
	case err == nil:
		// the text changed since: extracted again
		if _, err := tx.Exec(ctx, "DELETE FROM concept_index_lessons WHERE index_id = $1 AND lesson_key = $2", indexID, key); err != nil {
			return false, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}
	var from int64
	err = tx.QueryRow(ctx, `
		SELECT il.index_id FROM concept_index_lessons il JOIN concept_indexes i ON i.id = il.index_id
		WHERE i.course_id = $1 AND il.index_id <> $2 AND il.lesson_key = $3 AND il.text_hash = $4
		ORDER BY i.started_at DESC LIMIT 1`, courseID, indexID, key, hash).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO concept_index_lessons (index_id, lesson_key, text_hash) VALUES ($1, $2, $3)", indexID, key, hash); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary)
		SELECT $1, lesson_key, position, name, aliases, section_id, section_title, summary
		FROM concept_entries WHERE index_id = $2 AND lesson_key = $3`, indexID, from, key)
	return err == nil, err
}

// KeepLessonConcepts keeps what was extracted of a lesson of the build.
func (s *Service) KeepLessonConcepts(ctx context.Context, b ConceptIndexBuild, l ConceptIndexLesson, concepts []ExtractedConcept) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "INSERT INTO concept_index_lessons (index_id, lesson_key, text_hash) VALUES ($1, $2, $3)", b.ID, l.Key, l.hash); err != nil {
		return err
	}
	for i, c := range concepts {
		aliases := c.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, b.ID, l.Key, i, c.Name, aliases, c.SectionID, c.SectionTitle, c.Summary); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ConceptIndexEnd is how a build ended: built, or failed with a run error
// code.
type ConceptIndexEnd struct {
	status ConceptIndexStatus
	code   string
}

// ConceptIndexDone is a build that extracted every lesson.
func ConceptIndexDone() ConceptIndexEnd { return ConceptIndexEnd{status: ConceptIndexBuilt} }

// ConceptIndexFailedWith is a build that stopped at a lesson, with the run
// error code it failed with.
func ConceptIndexFailedWith(code string) ConceptIndexEnd {
	return ConceptIndexEnd{status: ConceptIndexFailed, code: code}
}

// FinishConceptIndex ends a build as end says. What a failed build
// extracted before it failed is kept for the next try.
func (s *Service) FinishConceptIndex(ctx context.Context, b ConceptIndexBuild, end ConceptIndexEnd) error {
	var reason any
	if end.status == ConceptIndexFailed {
		reason = end.code
	}
	_, err := s.db.Exec(ctx, "UPDATE concept_indexes SET status = $2, error = $3, finished_at = $4 WHERE id = $1", b.ID, end.status, reason, s.now())
	return err
}

// FailInterruptedConceptIndexes marks the builds a stopped server left behind as
// failed, so they can be tried again.
func (s *Service) FailInterruptedConceptIndexes(ctx context.Context) error {
	_, err := s.db.Exec(ctx, "UPDATE concept_indexes SET status = 'failed', error = 'interrupted', finished_at = $1 WHERE status = 'building'", s.now())
	return err
}
