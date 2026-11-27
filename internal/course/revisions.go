package course

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A revision's kind (#226).
const (
	RevisionConcept = "concept"
	RevisionNew     = "new"
	RevisionExclude = "exclude"
)

// The weights a revision gives a concept: on the concept path's RRF score,
// never forcing it first (ADR 0015).
var revisionWeights = []float64{0.5, 1, 2, 4}

var (
	// ErrRevisionNotFound: no such revision of the course.
	ErrRevisionNotFound = errors.New("revision not found")
	// ErrSectionNotFound: the lesson of the course's current version has
	// no such section, or there is no such lesson.
	ErrSectionNotFound = errors.New("section not found")
)

// Revision is an admin's revision of a course's concept index (#226),
// laid over recall: of a concept (Concept is its name as extracted), a
// concept added, or a lesson or section excluded. Name, Aliases, Summary
// and Generic are nil where a concept revision leaves them as extracted.
type Revision struct {
	ID       int64    `json:"id"`
	Kind     string   `json:"kind"`
	Concept  string   `json:"concept,omitempty"`
	Name     *string  `json:"name,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	Summary  *string  `json:"summary,omitempty"`
	Weight   float64  `json:"weight"`
	Disabled bool     `json:"disabled"`
	Generic  *bool    `json:"generic,omitempty"`
	// Lesson and Section are what it points at: the concept's primary
	// section when it was made, the section added at, the lesson or
	// section excluded. Lesson is nil when it points at nothing.
	Lesson  *EntryLesson  `json:"lesson"`
	Section *EntrySection `json:"section"`
	// Stale: the lesson's text changed since it was made or confirmed, or
	// the section is gone; it still applies until the admin confirms.
	Stale     bool      `json:"stale"`
	UpdatedAt time.Time `json:"updatedAt"`
	key       string
}

// ConceptRevision is how an admin revises a concept: nil leaves a field
// as extracted.
type ConceptRevision struct {
	Name     *string  `json:"name"`
	Aliases  []string `json:"aliases"`
	Summary  *string  `json:"summary"`
	Weight   float64  `json:"weight"`
	Disabled bool     `json:"disabled"`
	Generic  *bool    `json:"generic"`
}

// NewConcept is a concept an admin adds at a section of a lesson, the
// section by its heading in the course's current version.
type NewConcept struct {
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Summary      string   `json:"summary"`
	Weight       float64  `json:"weight"`
	LessonKey    string   `json:"lessonKey"`
	SectionTitle string   `json:"sectionTitle"`
}

// LessonSections is a lesson of a course's current version and its
// sections, which revisions may point at.
type LessonSections struct {
	Lesson   EntryLesson    `json:"lesson"`
	Sections []EntrySection `json:"sections"`
	hash     string
}

// SectionsOf is the lessons of a course's current version with their
// sections, from its passages, in course order.
func (s *Service) SectionsOf(ctx context.Context, courseID int64) ([]LessonSections, error) {
	var dir string
	if err := s.db.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 AND deleted_at IS NULL", courseID).Scan(&dir); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := s.PassagesOf(ctx, courseID, dir); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		WITH h AS (
			SELECT lesson_key, md5(string_agg(body, '' ORDER BY position)) AS hash
			FROM passages WHERE course_id = $1 AND files_dir = $2 GROUP BY lesson_key)
		SELECT l.id, l.key, l.title, p.section_id, p.section_title, h.hash
		FROM passages p
		JOIN h ON h.lesson_key = p.lesson_key
		JOIN lessons l ON l.course_id = p.course_id AND l.key = p.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE p.course_id = $1 AND p.files_dir = $2
		ORDER BY ch.position, l.position, p.position`, courseID, dir)
	if err != nil {
		return nil, err
	}
	out := []LessonSections{}
	var l EntryLesson
	var id, title, hash string
	_, err = pgx.ForEachRow(rows, []any{&l.ID, &l.Key, &l.Title, &id, &title, &hash}, func() error {
		if len(out) == 0 || out[len(out)-1].Lesson.Key != l.Key {
			out = append(out, LessonSections{Lesson: l, Sections: []EntrySection{}, hash: hash})
		}
		last := &out[len(out)-1]
		if id != "" && !slices.ContainsFunc(last.Sections, func(s EntrySection) bool { return s.ID == id }) {
			last.Sections = append(last.Sections, EntrySection{ID: id, Title: title})
		}
		return nil
	})
	return out, err
}

// RevisionsOf is a course's revisions, each marked stale when what it
// points at changed in the current version.
func (s *Service) RevisionsOf(ctx context.Context, courseID int64) ([]Revision, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, kind, concept_key, name, aliases, summary, weight, disabled, generic, lesson_key, section_id, section_title, text_hash, updated_at
		FROM index_revisions WHERE course_id = $1 ORDER BY kind, updated_at, id`, courseID)
	if err != nil {
		return nil, err
	}
	out := []Revision{}
	var r Revision
	var lessonKey, sectionID, sectionTitle, hash string
	hashes := map[int]string{}
	type pointer struct{ lesson, section, title string }
	points := map[int]pointer{}
	if _, err := pgx.ForEachRow(rows, []any{&r.ID, &r.Kind, &r.key, &r.Name, &r.Aliases, &r.Summary, &r.Weight, &r.Disabled, &r.Generic,
		&lessonKey, &sectionID, &sectionTitle, &hash, &r.UpdatedAt}, func() error {
		rr := r
		rr.Concept = r.key
		rr.Aliases = append([]string(nil), r.Aliases...)
		out = append(out, rr)
		points[len(out)-1] = pointer{lessonKey, sectionID, sectionTitle}
		hashes[len(out)-1] = hash
		return nil
	}); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	lessons, err := s.SectionsOf(ctx, courseID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		p := points[i]
		if p.lesson == "" {
			continue
		}
		at := slices.IndexFunc(lessons, func(l LessonSections) bool { return l.Lesson.Key == p.lesson })
		if p.section != "" {
			out[i].Section = &EntrySection{ID: p.section, Title: p.title}
		}
		if at < 0 {
			out[i].Lesson = &EntryLesson{Key: p.lesson, Title: p.lesson}
			out[i].Stale = true
			continue
		}
		l := lessons[at]
		out[i].Lesson = &EntryLesson{ID: l.Lesson.ID, Key: l.Lesson.Key, Title: l.Lesson.Title}
		gone := p.section != "" && !slices.ContainsFunc(l.Sections, func(s EntrySection) bool { return s.ID == p.section })
		out[i].Stale = gone || l.hash != hashes[i]
	}
	return out, nil
}

// revisions is a course's revisions as recall lays them over: what they
// point at by key and title, not checked against the current version.
func (s *Service) revisions(ctx context.Context, courseID int64) ([]Revision, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.kind, r.concept_key, r.name, r.aliases, r.summary, r.weight, r.disabled, r.generic,
		       r.lesson_key, coalesce(l.id, 0), coalesce(l.title, r.lesson_key), r.section_id, r.section_title
		FROM index_revisions r LEFT JOIN lessons l ON l.course_id = r.course_id AND l.key = r.lesson_key
		WHERE r.course_id = $1 ORDER BY r.id`, courseID)
	if err != nil {
		return nil, err
	}
	var out []Revision
	var r Revision
	var l EntryLesson
	var sectionID, sectionTitle string
	_, err = pgx.ForEachRow(rows, []any{&r.ID, &r.Kind, &r.key, &r.Name, &r.Aliases, &r.Summary, &r.Weight, &r.Disabled, &r.Generic,
		&l.Key, &l.ID, &l.Title, &sectionID, &sectionTitle}, func() error {
		rr := r
		rr.Aliases = append([]string(nil), r.Aliases...)
		if r.Aliases != nil && len(r.Aliases) == 0 {
			rr.Aliases = []string{}
		}
		rr.Lesson, rr.Section = nil, nil
		if l.Key != "" {
			ll := l
			rr.Lesson = &ll
		}
		if sectionID != "" {
			rr.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		out = append(out, rr)
		return nil
	})
	return out, err
}

// lessonAt is the lesson keyed key of the course's current version and
// the section headed title (or anchored id, when title is ""), "" for the
// whole lesson.
func (s *Service) lessonAt(ctx context.Context, courseID int64, key, id, title string) (LessonSections, *EntrySection, error) {
	lessons, err := s.SectionsOf(ctx, courseID)
	if err != nil {
		return LessonSections{}, nil, err
	}
	at := slices.IndexFunc(lessons, func(l LessonSections) bool { return l.Lesson.Key == key })
	if at < 0 {
		return LessonSections{}, nil, ErrSectionNotFound
	}
	l := lessons[at]
	if id == "" && title == "" {
		return l, nil, nil
	}
	for _, sec := range l.Sections {
		if title != "" && strings.TrimSpace(sec.Title) == strings.TrimSpace(title) || title == "" && sec.ID == id {
			return l, &sec, nil
		}
	}
	return l, nil, ErrSectionNotFound
}

func validWeight(w float64) bool { return slices.Contains(revisionWeights, w) }

// libraryCourseExists is ErrNotFound unless the course is in the library.
func (s *Service) libraryCourseExists(ctx context.Context, courseID int64) error {
	var ok bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND visibility = 'public' AND deleted_at IS NULL)", courseID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ReviseConcept keeps an admin's revision of the concept extracted as
// concept, replacing an earlier one; it points at the concept's primary
// section in the current index, when there is one.
func (s *Service) ReviseConcept(ctx context.Context, courseID int64, concept string, r ConceptRevision) error {
	key := NormalizeConcept(concept)
	if key == "" || !validWeight(r.Weight) || r.Name != nil && strings.TrimSpace(*r.Name) == "" {
		return ErrBadInput
	}
	if err := s.libraryCourseExists(ctx, courseID); err != nil {
		return err
	}
	var dir string
	if err := s.db.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1", courseID).Scan(&dir); err != nil {
		return err
	}
	concepts, err := s.ConceptsOf(ctx, courseID, dir)
	if err != nil {
		return err
	}
	var lessonKey, sectionID, sectionTitle, hash string
	if at := slices.IndexFunc(concepts, func(c IndexConcept) bool { return NormalizeConcept(c.Name) == key }); at >= 0 {
		p := concepts[at].Primary
		lessonKey = p.Lesson.Key
		if p.Section != nil {
			sectionID, sectionTitle = p.Section.ID, p.Section.Title
		}
		l, _, err := s.lessonAt(ctx, courseID, lessonKey, "", "")
		if err == nil {
			hash = l.hash
		}
	}
	if r.Name != nil {
		name := strings.TrimSpace(*r.Name)
		r.Name = &name
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO index_revisions (course_id, kind, concept_key, name, aliases, summary, weight, disabled, generic, lesson_key, section_id, section_title, text_hash, updated_at)
		VALUES ($1, 'concept', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (course_id, concept_key) WHERE kind = 'concept' DO UPDATE SET name = $3, aliases = $4, summary = $5, weight = $6, disabled = $7, generic = $8,
			lesson_key = $9, section_id = $10, section_title = $11, text_hash = $12, updated_at = $13`,
		courseID, key, r.Name, cleanAliases(r.Aliases), r.Summary, r.Weight, r.Disabled, r.Generic, lessonKey, sectionID, sectionTitle, hash, s.now())
	return err
}

// cleanAliases trims aliases and drops the empty ones; nil stays nil (as
// extracted).
func cleanAliases(aliases []string) []string {
	if aliases == nil {
		return nil
	}
	out := []string{}
	for _, a := range aliases {
		if a = strings.TrimSpace(a); a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// AddConcept keeps a concept an admin added at a section of a lesson of
// the current version, by its heading; one of the same name is replaced.
func (s *Service) AddConcept(ctx context.Context, courseID int64, c NewConcept) (int64, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || !validWeight(c.Weight) {
		return 0, ErrBadInput
	}
	if err := s.libraryCourseExists(ctx, courseID); err != nil {
		return 0, err
	}
	l, sec, err := s.lessonAt(ctx, courseID, c.LessonKey, "", c.SectionTitle)
	if err != nil {
		return 0, err
	}
	var sectionID, sectionTitle string
	if sec != nil {
		sectionID, sectionTitle = sec.ID, sec.Title
	}
	var id int64
	err = s.db.QueryRow(ctx, `
		INSERT INTO index_revisions (course_id, kind, concept_key, name, aliases, summary, weight, lesson_key, section_id, section_title, text_hash, updated_at)
		VALUES ($1, 'new', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (course_id, concept_key) WHERE kind = 'new' DO UPDATE SET name = $3, aliases = $4, summary = $5, weight = $6,
			lesson_key = $7, section_id = $8, section_title = $9, text_hash = $10, updated_at = $11
		RETURNING id`,
		courseID, NormalizeConcept(c.Name), c.Name, cleanAliases(append([]string{}, c.Aliases...)), strings.TrimSpace(c.Summary), c.Weight,
		l.Lesson.Key, sectionID, sectionTitle, l.hash, s.now()).Scan(&id)
	return id, err
}

// Exclude leaves a lesson of the current version (sectionID "") or one of
// its sections out of recall.
func (s *Service) Exclude(ctx context.Context, courseID int64, lessonKey, sectionID string) (int64, error) {
	if err := s.libraryCourseExists(ctx, courseID); err != nil {
		return 0, err
	}
	l, sec, err := s.lessonAt(ctx, courseID, lessonKey, sectionID, "")
	if err != nil {
		return 0, err
	}
	var title string
	if sec != nil {
		title = sec.Title
	}
	var id int64
	err = s.db.QueryRow(ctx, `
		INSERT INTO index_revisions (course_id, kind, lesson_key, section_id, section_title, text_hash, updated_at)
		VALUES ($1, 'exclude', $2, $3, $4, $5, $6)
		ON CONFLICT (course_id, lesson_key, section_id) WHERE kind = 'exclude' DO UPDATE SET section_title = $4, text_hash = $5, updated_at = $6
		RETURNING id`, courseID, l.Lesson.Key, sectionID, title, l.hash, s.now()).Scan(&id)
	return id, err
}

// DeleteRevision takes a revision of the course back.
func (s *Service) DeleteRevision(ctx context.Context, courseID, id int64) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM index_revisions WHERE course_id = $1 AND id = $2", courseID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrRevisionNotFound
	}
	return err
}

// ConfirmRevision has the admin confirm a revision that may be outdated:
// it points at the lesson's text as it is now. One whose section is gone
// stays outdated.
func (s *Service) ConfirmRevision(ctx context.Context, courseID, id int64) error {
	var key string
	err := s.db.QueryRow(ctx, "SELECT lesson_key FROM index_revisions WHERE course_id = $1 AND id = $2", courseID, id).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRevisionNotFound
	}
	if err != nil {
		return err
	}
	var hash string
	if l, _, err := s.lessonAt(ctx, courseID, key, "", ""); err == nil {
		hash = l.hash
	} else if !errors.Is(err, ErrSectionNotFound) {
		return err
	}
	_, err = s.db.Exec(ctx, "UPDATE index_revisions SET text_hash = $3, updated_at = $4 WHERE course_id = $1 AND id = $2", courseID, id, hash, s.now())
	return err
}
