package course

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// genericShare is the share of an index's lessons a concept may appear in
// before it is generic and left out of recall (ADR 0015); genericFloor is
// the fewest lessons a generic one appears in, so that a small course does
// not lose every concept two of its lessons share.
const (
	genericShare = 0.35
	genericFloor = 3
)

// IndexConcept is a concept of an index (#225): the entries of the
// lessons merged, where it is properly explained (its primary section),
// and whether it is generic.
type IndexConcept struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Summary string   `json:"summary"`
	Generic bool     `json:"generic"`
	// Lessons is how many lessons it appears in.
	Lessons int `json:"lessons"`
	// Primary is where it is properly explained: its lesson and section.
	Primary     ConceptOccurrence   `json:"primary"`
	Occurrences []ConceptOccurrence `json:"occurrences"`
}

// ConceptOccurrence is where a concept appears: a lesson and section, what
// the lesson does with it, and what it says it is there.
type ConceptOccurrence struct {
	Lesson  EntryLesson   `json:"lesson"`
	Section *EntrySection `json:"section"`
	Role    string        `json:"role"`
	Summary string        `json:"summary"`
}

// merged is a concept as merging made it, with the entries it holds by
// their place in the list merged.
type merged struct {
	IndexConcept
	members []int
	// head is the entry the concept is named after: the first of the
	// name the others joined
	head int
}

// mergeEntries merges an index's entries, in course order, into concepts
// (#225), in the order they first appear. Entries of the same name are one
// concept; so is an entry with another's name as an alias, joined to it.
// Joining goes one step only: a concept that joined another takes no one
// in, and one that took others in joins no one, so that names do not
// chain (an alias of a third concept never makes two concepts one). Where
// a concept could join several, it joins the one of most entries.
func mergeEntries(entries []ConceptEntry, lessons int) []merged {
	type group struct {
		name    string
		members []int
		aliases map[string]bool
	}
	var groups []*group
	byName := map[string]*group{}
	for i, e := range entries {
		n := NormalizeConcept(e.Name)
		g := byName[n]
		if g == nil {
			g = &group{name: n, aliases: map[string]bool{}}
			byName[n] = g
			groups = append(groups, g)
		}
		g.members = append(g.members, i)
		for _, a := range e.Aliases {
			if a := NormalizeConcept(a); a != n {
				g.aliases[a] = true
			}
		}
	}
	type join struct{ from, to int }
	at := map[*group]int{}
	for i, g := range groups {
		at[g] = i
	}
	var joins []join
	for i, g := range groups {
		for a := range g.aliases {
			if to, ok := byName[a]; ok && to != g {
				joins = append(joins, join{i, at[to]})
			}
		}
	}
	slices.SortFunc(joins, func(a, b join) int {
		if d := len(groups[b.to].members) - len(groups[a.to].members); d != 0 {
			return d
		}
		if a.to != b.to {
			return a.to - b.to
		}
		return a.from - b.from
	})
	into := map[int]int{}
	host := map[int]bool{}
	for _, j := range joins {
		if _, joined := into[j.from]; joined || host[j.from] {
			continue
		}
		if _, joined := into[j.to]; joined {
			continue
		}
		into[j.from] = j.to
		host[j.to] = true
	}
	var out []merged
	place := map[int]int{}
	for i, g := range groups {
		if _, joined := into[i]; joined {
			continue
		}
		place[i] = len(out)
		out = append(out, merged{members: append([]int{}, g.members...), head: g.members[0]})
	}
	for from, to := range into {
		m := &out[place[to]]
		m.members = append(m.members, groups[from].members...)
	}
	for i := range out {
		m := &out[i]
		slices.Sort(m.members)
		m.Name = entries[m.head].Name
		seen := map[string]bool{NormalizeConcept(m.Name): true}
		m.Aliases = []string{}
		inLessons := map[string]bool{}
		for _, ei := range m.members {
			e := entries[ei]
			for _, n := range append([]string{e.Name}, e.Aliases...) {
				if k := NormalizeConcept(n); !seen[k] {
					seen[k] = true
					m.Aliases = append(m.Aliases, n)
				}
			}
			inLessons[e.Lesson.Key] = true
			m.Occurrences = append(m.Occurrences, ConceptOccurrence{Lesson: e.Lesson, Section: e.Section, Role: e.Role, Summary: e.Summary})
		}
		m.Lessons = len(inLessons)
		m.Generic = m.Lessons >= genericFloor && float64(m.Lessons) > genericShare*float64(lessons)
		m.Primary = primaryOf(m.Occurrences, m.Name, nil)
		m.Summary = m.Primary.Summary
	}
	return out
}

// primaryOf is the occurrence that explains concept name best, of those
// skip leaves (nil: all): one whose lesson defines it and whose lesson
// title names it, then one that defines it, then one whose lesson title
// names it, the first in course order of those. With every occurrence
// skipped, it is the first.
func primaryOf(occ []ConceptOccurrence, name string, skip func(ConceptOccurrence) bool) ConceptOccurrence {
	n := NormalizeConcept(name)
	best, bestScore := -1, -1
	for i, o := range occ {
		if skip != nil && skip(o) {
			continue
		}
		score := 0
		if o.Role != "use" {
			score += 2
		}
		if strings.Contains(NormalizeConcept(o.Lesson.Title), n) {
			score++
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return occ[0]
	}
	return occ[best]
}

// MergeConcepts merges the entries of a build's index into its concepts,
// replacing those of an earlier build (#225).
func (s *Service) MergeConcepts(ctx context.Context, b ConceptIndexBuild) error {
	x, err := s.entriesOf(ctx, b.ID, b.CourseID)
	if err != nil {
		return err
	}
	var lessons int
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM concept_index_lessons WHERE index_id = $1", b.ID).Scan(&lessons); err != nil {
		return err
	}
	concepts := mergeEntries(x.entries, lessons)
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM index_concepts WHERE index_id = $1", b.ID); err != nil {
			return err
		}
		for i, c := range concepts {
			var sectionID, sectionTitle string
			if c.Primary.Section != nil {
				sectionID, sectionTitle = c.Primary.Section.ID, c.Primary.Section.Title
			}
			var id int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO index_concepts (index_id, position, name, aliases, summary, generic, lessons, primary_lesson_key, primary_section_id, primary_section_title)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
				b.ID, i, c.Name, c.Aliases, c.Summary, c.Generic, c.Lessons, c.Primary.Lesson.Key, sectionID, sectionTitle).Scan(&id); err != nil {
				return err
			}
			ids := make([]int64, len(c.members))
			for j, ei := range c.members {
				ids[j] = x.ids[ei]
			}
			if _, err := tx.Exec(ctx, "UPDATE concept_entries SET concept_id = $2 WHERE id = ANY($1)", ids, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// indexEntries are an index's entries in course order, with their ids.
type indexEntries struct {
	entries []ConceptEntry
	ids     []int64
}

func (s *Service) entriesOf(ctx context.Context, indexID, courseID int64) (indexEntries, error) {
	var out indexEntries
	rows, err := s.db.Query(ctx, `
		SELECT e.id, e.name, e.aliases, l.id, l.key, l.title, e.section_id, e.section_title, e.summary, e.role
		FROM concept_entries e
		JOIN lessons l ON l.course_id = $2 AND l.key = e.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE e.index_id = $1
		ORDER BY ch.position, l.position, e.position`, indexID, courseID)
	if err != nil {
		return out, err
	}
	var id int64
	var e ConceptEntry
	var sectionID, sectionTitle string
	_, err = pgx.ForEachRow(rows, []any{&id, &e.Name, &e.Aliases, &e.Lesson.ID, &e.Lesson.Key, &e.Lesson.Title, &sectionID, &sectionTitle, &e.Summary, &e.Role}, func() error {
		entry := e
		entry.Aliases = append([]string{}, e.Aliases...)
		entry.Section = nil
		if sectionID != "" {
			entry.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		out.entries = append(out.entries, entry)
		out.ids = append(out.ids, id)
		return nil
	})
	return out, err
}

// ConceptsOf is the concepts of a version's index, in the order they first
// appear, each with where it appears.
func (s *Service) ConceptsOf(ctx context.Context, courseID int64, filesDir string) ([]IndexConcept, error) {
	out := []IndexConcept{}
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.name, c.aliases, c.summary, c.generic, c.lessons,
		       e.lesson_key, l.id, l.title, e.section_id, e.section_title, e.role, e.summary,
		       c.primary_lesson_key, c.primary_section_id
		FROM concept_indexes i
		JOIN index_concepts c ON c.index_id = i.id
		JOIN concept_entries e ON e.concept_id = c.id
		JOIN lessons l ON l.course_id = i.course_id AND l.key = e.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE i.course_id = $1 AND i.files_dir = $2
		ORDER BY c.position, ch.position, l.position, e.position`, courseID, filesDir)
	if err != nil {
		return nil, err
	}
	var c IndexConcept
	var o ConceptOccurrence
	var sectionID, sectionTitle, primaryKey, primarySection string
	_, err = pgx.ForEachRow(rows, []any{&c.ID, &c.Name, &c.Aliases, &c.Summary, &c.Generic, &c.Lessons,
		&o.Lesson.Key, &o.Lesson.ID, &o.Lesson.Title, &sectionID, &sectionTitle, &o.Role, &o.Summary, &primaryKey, &primarySection}, func() error {
		if len(out) == 0 || out[len(out)-1].ID != c.ID {
			cc := c
			cc.Aliases = append([]string{}, c.Aliases...)
			cc.Occurrences = nil
			out = append(out, cc)
		}
		last := &out[len(out)-1]
		occ := o
		occ.Section = nil
		if sectionID != "" {
			occ.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		last.Occurrences = append(last.Occurrences, occ)
		if occ.Lesson.Key == primaryKey && sectionID == primarySection && last.Primary.Lesson.Key == "" {
			last.Primary = occ
		}
		return nil
	})
	return out, err
}
