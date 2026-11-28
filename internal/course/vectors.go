package course

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

// HitVector: the answer means much the same as a passage of the section
// (cosine similarity of their embeddings, #227).
const HitVector HitSource = "vector"

// PassageText is a passage to embed: where it is, and its text.
type PassageText struct {
	CourseID  int64
	FilesDir  string
	LessonKey string
	Position  int
	Body      string
}

// PassagesToEmbed are up to limit passages of courses' current versions
// with no vector of generation yet, course by course.
func (s *Service) PassagesToEmbed(ctx context.Context, generation int64, limit int) ([]PassageText, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.course_id, p.files_dir, p.lesson_key, p.position, p.body
		FROM passages p JOIN courses c ON c.id = p.course_id AND c.files_dir = p.files_dir AND c.deleted_at IS NULL
		LEFT JOIN passage_vectors v ON v.course_id = p.course_id AND v.files_dir = p.files_dir AND v.lesson_key = p.lesson_key AND v.position = p.position
		WHERE v.generation IS DISTINCT FROM $1
		ORDER BY p.course_id, p.lesson_key, p.position
		LIMIT $2`, generation, limit)
	if err != nil {
		return nil, err
	}
	var out []PassageText
	var p PassageText
	_, err = pgx.ForEachRow(rows, []any{&p.CourseID, &p.FilesDir, &p.LessonKey, &p.Position, &p.Body}, func() error {
		out = append(out, p)
		return nil
	})
	return out, err
}

// KeepVectors keeps passages' vectors of generation, in the order of ps.
func (s *Service) KeepVectors(ctx context.Context, generation int64, ps []PassageText, vectors [][]float32) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		for i, p := range ps {
			if _, err := tx.Exec(ctx, `
				INSERT INTO passage_vectors (course_id, files_dir, lesson_key, position, generation, vector) VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (course_id, files_dir, lesson_key, position) DO UPDATE SET generation = $5, vector = $6`,
				p.CourseID, p.FilesDir, p.LessonKey, p.Position, generation, vectors[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// EmbeddingProgress is how far a course's current version is embedded:
// its passages with a vector of the current generation, of all of them.
type EmbeddingProgress struct {
	CourseID int64  `json:"courseId"`
	Title    string `json:"title"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
}

// EmbeddingProgressOf is how far each library course's current version is
// embedded in generation, by title.
func (s *Service) EmbeddingProgressOf(ctx context.Context, generation int64) ([]EmbeddingProgress, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.title,
		       (SELECT count(*) FROM passage_vectors v WHERE v.course_id = c.id AND v.files_dir = c.files_dir AND v.generation = $1),
		       (SELECT count(*) FROM passages p WHERE p.course_id = c.id AND p.files_dir = c.files_dir)
		FROM courses c WHERE c.visibility = 'public' AND c.deleted_at IS NULL ORDER BY c.title, c.id`, generation)
	if err != nil {
		return nil, err
	}
	out := []EmbeddingProgress{}
	var p EmbeddingProgress
	_, err = pgx.ForEachRow(rows, []any{&p.CourseID, &p.Title, &p.Done, &p.Total}, func() error {
		out = append(out, p)
		return nil
	})
	return out, err
}

// withVectors gives the passages their vectors of generation, where they
// have one.
func (s *Service) withVectors(ctx context.Context, p *Passages, courseID int64, filesDir string, generation int64) error {
	rows, err := s.db.Query(ctx, `
		SELECT lesson_key, position, vector FROM passage_vectors
		WHERE course_id = $1 AND files_dir = $2 AND generation = $3`, courseID, filesDir, generation)
	if err != nil {
		return err
	}
	type at struct {
		key string
		pos int
	}
	vs := map[at][]float32{}
	var key string
	var pos int
	var v []float32
	if _, err := pgx.ForEachRow(rows, []any{&key, &pos, &v}, func() error {
		vs[at{key, pos}] = append([]float32(nil), v...)
		return nil
	}); err != nil {
		return err
	}
	for i := range p.list {
		p.list[i].vector = vs[at{p.list[i].Lesson.Key, p.list[i].position}]
	}
	return nil
}

// rankVector scores every passage with a vector outside the lesson keyed
// exceptLesson by its cosine similarity to q, best first.
func (p Passages) rankVector(q []float32, exceptLesson string) []PathHit {
	type scored struct {
		at    int
		score float64
	}
	var found []scored
	for i, ps := range p.list {
		if ps.vector == nil || ps.Lesson.Key == exceptLesson {
			continue
		}
		found = append(found, scored{i, dot(q, ps.vector)})
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

// dot is the dot product of two vectors, the cosine of unit ones; 0 for
// vectors of different lengths.
func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var d float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
	}
	return d
}

// EmbeddingProgressFor is how far a version of a course is embedded in
// generation.
func (s *Service) EmbeddingProgressFor(ctx context.Context, courseID int64, filesDir string, generation int64) (EmbeddingProgress, error) {
	p := EmbeddingProgress{CourseID: courseID}
	err := s.db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM passage_vectors WHERE course_id = $1 AND files_dir = $2 AND generation = $3),
		       (SELECT count(*) FROM passages WHERE course_id = $1 AND files_dir = $2)`, courseID, filesDir, generation).Scan(&p.Done, &p.Total)
	return p, err
}
