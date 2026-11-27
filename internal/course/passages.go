package course

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/storage"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// A passage is a window of passageWindow characters of a section's text;
// the next starts passageStep characters later (ADR 0015).
const (
	passageWindow = 700
	passageStep   = 600
)

// BM25's parameters (#223).
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// HitPassage: the answer shares words with a passage of the section (BM25).
const HitPassage HitSource = "passage"

// SectionPassage is a piece of a section's text, with its words counted for BM25.
type SectionPassage struct {
	Lesson EntryLesson
	// Section is nil for the text above the lesson's first heading.
	Section *EntrySection
	Body    string
	terms   map[string]int
	length  int
}

// Passages are a course version's passages, in course order, ready to be
// ranked against a text.
type Passages struct {
	list   []SectionPassage
	df     map[string]int
	avgLen float64
}

func newPassages(list []SectionPassage) Passages {
	p := Passages{list: list, df: map[string]int{}}
	total := 0
	for i := range p.list {
		terms := map[string]int{}
		words := passageTerms(p.list[i].Body)
		for _, w := range words {
			terms[w]++
		}
		for w := range terms {
			p.df[w]++
		}
		p.list[i].terms, p.list[i].length = terms, len(words)
		total += len(words)
	}
	if len(p.list) > 0 {
		p.avgLen = float64(total) / float64(len(p.list))
	}
	return p
}

// Len is how many passages there are.
func (p Passages) Len() int { return len(p.list) }

// rank scores every passage outside the lesson keyed exceptLesson against
// text with BM25 (each of the text's terms counted once), best first;
// passages that share no term with it are left out.
func (p Passages) rank(text, exceptLesson string) []PathHit {
	query := map[string]bool{}
	for _, w := range passageTerms(text) {
		query[w] = true
	}
	n := float64(len(p.list))
	type scored struct {
		at    int
		score float64
	}
	var found []scored
	for i, ps := range p.list {
		if ps.Lesson.Key == exceptLesson {
			continue
		}
		score := 0.0
		for w := range query {
			tf := float64(ps.terms[w])
			if tf == 0 {
				continue
			}
			df := float64(p.df[w])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*float64(ps.length)/p.avgLen))
		}
		if score > 0 {
			found = append(found, scored{i, score})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	hits := make([]PathHit, 0, len(found))
	for _, f := range found {
		ps := p.list[f.at]
		hits = append(hits, PathHit{Lesson: ps.Lesson, Section: ps.Section, Score: f.score})
	}
	return hits
}

// passageTerms are the words BM25 counts in s, normalized (NFKC, lower
// case): runs of Chinese, Japanese or Korean characters as overlapping
// pairs of characters (a lone character as itself), other letters and
// digits as words.
func passageTerms(s string) []string {
	var out []string
	var run []rune
	cjk := false
	flush := func() {
		switch {
		case len(run) == 0:
		case !cjk:
			out = append(out, string(run))
		case len(run) == 1:
			out = append(out, string(run))
		default:
			for i := 0; i+1 < len(run); i++ {
				out = append(out, string(run[i:i+2]))
			}
		}
		run = run[:0]
	}
	for _, r := range strings.ToLower(norm.NFKC.String(s)) {
		isCJK := unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
		word := isCJK || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !word || isCJK != cjk {
			flush()
		}
		if word {
			cjk = isCJK
			run = append(run, r)
		}
	}
	flush()
	return out
}

// cutPassage is a passage of a lesson's text as cut, before it is stored.
type cutPassage struct {
	sectionID, sectionTitle, body string
}

// cutPassages cuts a lesson's text into passages: the text from each
// heading to the next (and the text above the first) in windows of
// passageWindow characters, each passageStep after the last.
func cutPassages(markdown string) []cutPassage {
	type part struct {
		id, title  string
		start, end int
	}
	sections := anchor.Sections(markdown)
	var parts []part
	first := len(markdown)
	if len(sections) > 0 {
		first = sections[0].Start
	}
	parts = append(parts, part{start: 0, end: first})
	for i, sec := range sections {
		end := len(markdown)
		if i+1 < len(sections) {
			end = sections[i+1].Start
		}
		parts = append(parts, part{sec.ID, sec.Title, sec.Start, end})
	}
	var out []cutPassage
	for _, p := range parts {
		text := []rune(strings.TrimSpace(markdown[p.start:p.end]))
		if len(text) == 0 {
			continue
		}
		for from := 0; ; from += passageStep {
			to := min(from+passageWindow, len(text))
			out = append(out, cutPassage{p.id, p.title, string(text[from:to])})
			if to == len(text) {
				break
			}
		}
	}
	return out
}

// PassagesOf is the passages of a version of a course (its files folder).
// A course's current version without them yet gets them cut first; an
// older version without them has none. It does not check who may read the
// course.
func (s *Service) PassagesOf(ctx context.Context, courseID int64, filesDir string) (Passages, error) {
	var built bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM passage_sets WHERE course_id = $1 AND files_dir = $2)", courseID, filesDir).Scan(&built); err != nil {
		return Passages{}, err
	}
	if !built {
		if err := s.cutVersion(ctx, courseID, filesDir); err != nil {
			return Passages{}, err
		}
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.id, l.key, l.title, p.section_id, p.section_title, p.body
		FROM passages p
		JOIN lessons l ON l.course_id = p.course_id AND l.key = p.lesson_key
		JOIN chapters ch ON ch.id = l.chapter_id
		WHERE p.course_id = $1 AND p.files_dir = $2
		ORDER BY ch.position, l.position, p.position`, courseID, filesDir)
	if err != nil {
		return Passages{}, err
	}
	var list []SectionPassage
	var ps SectionPassage
	var sectionID, sectionTitle string
	_, err = pgx.ForEachRow(rows, []any{&ps.Lesson.ID, &ps.Lesson.Key, &ps.Lesson.Title, &sectionID, &sectionTitle, &ps.Body}, func() error {
		p := ps
		if sectionID != "" {
			p.Section = &EntrySection{ID: sectionID, Title: sectionTitle}
		}
		list = append(list, p)
		return nil
	})
	return newPassages(list), err
}

// CutMissingPassages cuts the passages of every course's current version
// that has none yet: a version just made, or one from before passages
// were (#223). A course that fails is logged and the rest go on.
func (s *Service) CutMissingPassages(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.files_dir FROM courses c
		WHERE c.deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM passage_sets p WHERE p.course_id = c.id AND p.files_dir = c.files_dir)
		ORDER BY c.id`)
	if err != nil {
		return err
	}
	type version struct {
		id  int64
		dir string
	}
	var missing []version
	var v version
	if _, err := pgx.ForEachRow(rows, []any{&v.id, &v.dir}, func() error {
		missing = append(missing, v)
		return nil
	}); err != nil {
		return err
	}
	for _, v := range missing {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.cutVersion(ctx, v.id, v.dir); err != nil {
			slog.Warn("passages not cut", "course", v.id, "dir", v.dir, "err", err)
		}
	}
	return nil
}

// cutVersion cuts and keeps the passages of a course's version, when it is
// still the course's current one and has none. The texts are read with no
// lock held; a lesson whose text is missing is logged and left out.
func (s *Service) cutVersion(ctx context.Context, courseID int64, dir string) error {
	var current bool
	if err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND files_dir = $2 AND deleted_at IS NULL)", courseID, dir).Scan(&current); err != nil || !current {
		return err
	}
	rows, err := s.db.Query(ctx, `
		SELECT l.key, l.file FROM lessons l JOIN chapters ch ON ch.id = l.chapter_id
		WHERE l.course_id = $1 AND l.file IS NOT NULL ORDER BY ch.position, l.position`, courseID)
	if err != nil {
		return err
	}
	var key, file string
	type lessonFile struct{ key, file string }
	var files []lessonFile
	if _, err := pgx.ForEachRow(rows, []any{&key, &file}, func() error {
		files = append(files, lessonFile{key, file})
		return nil
	}); err != nil {
		return err
	}
	var copyRows [][]any
	for _, f := range files {
		t, err := s.readText(ctx, courseID, dir, LessonText{File: f.file})
		if errors.Is(err, storage.ErrNotFound) {
			slog.Warn("lesson text missing", "course", courseID, "file", filesPrefix(courseID, dir)+f.file)
			continue
		}
		if err != nil {
			return err
		}
		for i, p := range cutPassages(t.Markdown) {
			copyRows = append(copyRows, []any{courseID, dir, f.key, i, p.sectionID, p.sectionTitle, p.body})
		}
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		// the texts were read from dir: under the lock it must still be the
		// course's version, or the passages are another version's to cut
		var now string
		err := tx.QueryRow(ctx, "SELECT files_dir FROM courses WHERE id = $1 AND deleted_at IS NULL FOR SHARE", courseID).Scan(&now)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && now != dir {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "INSERT INTO passage_sets (course_id, files_dir, built_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", courseID, dir, s.now())
		if err != nil || tag.RowsAffected() == 0 {
			return err // cut by another request meanwhile
		}
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"passages"},
			[]string{"course_id", "files_dir", "lesson_key", "position", "section_id", "section_title", "body"}, pgx.CopyFromRows(copyRows))
		return err
	})
}
