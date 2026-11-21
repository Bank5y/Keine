package course

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/auth"
)

// ErrOverlap: the selection overlaps one of the reader's explorations on
// the lesson; a stretch of text belongs to one exploration at most
// (ADR 0007).
var ErrOverlap = errors.New("selection overlaps an exploration")

// ExplorationState is where an exploration stands after course updates,
// as an annotation's does: only attached ones show in the text.
type ExplorationState string

const (
	ExplorationAttached ExplorationState = "attached"
	ExplorationPending  ExplorationState = "pending"
	ExplorationExcerpt  ExplorationState = "excerpt"
)

// Exploration is one of the viewer's explorations on a lesson, as the
// reading page shows it: its place, and the head of its document.
type Exploration struct {
	ID     int64            `json:"id"`
	Quote  string           `json:"quote"`
	Prefix string           `json:"prefix"`
	Suffix string           `json:"suffix"`
	State  ExplorationState `json:"state"`
	Title  string           `json:"title"`
	Intro  string           `json:"intro"`
	// Range is where the quote sits in the lesson text as it is now.
	Range     *anchor.Range `json:"range"`
	CreatedAt time.Time     `json:"createdAt"`
}

// overlaps tells whether two ranges of a text share a character.
func overlaps(a, b anchor.Range) bool { return a.Start < b.End && b.Start < a.End }

// Explorations lists the viewer's explorations on a lesson that are attached
// and found in its text as it is now, in the order they were made, each
// with its range. A lesson that takes no annotations has none.
func (s *Service) Explorations(ctx context.Context, viewer auth.User, courseID, lessonID int64) ([]Exploration, error) {
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if errors.Is(err, ErrNotAnnotatable) {
		return []Exploration{}, nil
	}
	if err != nil {
		return nil, err
	}
	return placedExplorations(ctx, s.db, viewer.ID, courseID, l.Key, text)
}

func placedExplorations(ctx context.Context, q querier, userID, courseID int64, lessonKey string, text anchor.Text) ([]Exploration, error) {
	rows, err := q.Query(ctx, `SELECT id, quote, prefix, suffix, state, title, intro, created_at FROM explorations
		WHERE user_id = $1 AND course_id = $2 AND lesson_key = $3 AND state = 'attached' ORDER BY created_at, id`, userID, courseID, lessonKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exploration{}
	for rows.Next() {
		var e Exploration
		if err := rows.Scan(&e.ID, &e.Quote, &e.Prefix, &e.Suffix, &e.State, &e.Title, &e.Intro, &e.CreatedAt); err != nil {
			return nil, err
		}
		if r, ok := text.Locate(anchor.Quote{Exact: e.Quote, Prefix: e.Prefix, Suffix: e.Suffix}); ok {
			e.Range = &r
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// overlapping returns ErrOverlap when r overlaps one of the user's
// explorations shown in the lesson text.
func overlapping(ctx context.Context, q querier, userID, courseID int64, lessonKey string, text anchor.Text, r anchor.Range) error {
	list, err := placedExplorations(ctx, q, userID, courseID, lessonKey, text)
	if err != nil {
		return err
	}
	for _, e := range list {
		if overlaps(*e.Range, r) {
			return ErrOverlap
		}
	}
	return nil
}

// CheckOverlap returns ErrOverlap when a passage overlaps one of the
// viewer's explorations on its lesson.
func (s *Service) CheckOverlap(ctx context.Context, viewer auth.User, p Passage) error {
	return overlapping(ctx, s.db, viewer.ID, p.Course.ID, p.Lesson.Key, anchor.MarkdownText(p.Lesson.Markdown), p.Range)
}

// limits on an exploration document's head, in characters (#123)
const (
	maxTitle = 100
	maxIntro = 300
	// the main node is named after the start of the selection
	maxNodeName = 40
)

// Pin is an analysis to keep as an exploration: the selection it was
// made on, its three parts, the request it ran on and its reply, and the
// turn's metadata. Snapshot and Meta are stored as JSON.
type Pin struct {
	Quote                 anchor.Quote
	Title, Intro, Meaning string
	Snapshot              any
	Reply                 string
	Meta                  any
}

// Pin keeps an analysis as one of the viewer's explorations: in one
// transaction the exploration with its document, its main node, and the
// analysis as the node's first turn. The selection must still be in the
// lesson text and overlap none of the viewer's explorations, whatever was
// made there since the analysis.
func (s *Service) Pin(ctx context.Context, viewer auth.User, courseID, lessonID int64, in Pin) (Exploration, error) {
	if in.Title == "" || in.Intro == "" || utf8.RuneCountInString(in.Title) > maxTitle || utf8.RuneCountInString(in.Intro) > maxIntro ||
		utf8.RuneCountInString(in.Meaning) > maxNote || !validQuote(in.Quote.Exact, in.Quote.Prefix, in.Quote.Suffix) {
		return Exploration{}, ErrBadInput
	}
	snapshot, err := json.Marshal(in.Snapshot)
	if err != nil {
		return Exploration{}, err
	}
	meta, err := json.Marshal(in.Meta)
	if err != nil {
		return Exploration{}, err
	}
	question, _ := json.Marshal(map[string]any{"text": "", "attachments": []map[string]string{{"kind": "selection", "text": in.Quote.Exact}}})
	l, text, err := s.annotatable(ctx, viewer, courseID, lessonID)
	if err != nil {
		return Exploration{}, err
	}
	r, ok := text.Locate(in.Quote)
	if !ok {
		return Exploration{}, ErrQuoteNotFound
	}
	e := Exploration{Quote: in.Quote.Exact, Prefix: in.Quote.Prefix, Suffix: in.Quote.Suffix, State: ExplorationAttached, Title: in.Title, Intro: in.Intro, Range: &r}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		// one pin of the viewer's at a time, so two never overlap
		if _, err := tx.Exec(ctx, "SELECT 1 FROM users WHERE id = $1 FOR UPDATE", viewer.ID); err != nil {
			return err
		}
		if err := overlapping(ctx, tx, viewer.ID, courseID, l.Key, text, r); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO explorations (user_id, course_id, lesson_key, quote, prefix, suffix, title, intro, meaning, snapshot)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
			viewer.ID, courseID, l.Key, in.Quote.Exact, in.Quote.Prefix, in.Quote.Suffix, in.Title, in.Intro, in.Meaning, snapshot).Scan(&e.ID, &e.CreatedAt); err != nil {
			return err
		}
		var node int64
		if err := tx.QueryRow(ctx, "INSERT INTO exploration_nodes (exploration_id, kind, name) VALUES ($1, 'main', $2) RETURNING id",
			e.ID, nodeName(in.Quote.Exact)).Scan(&node); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO exploration_turns (node_id, question, reply, meta) VALUES ($1, $2, $3, $4)", node, question, in.Reply, meta)
		return err
	})
	return e, err
}

// nodeName names a node after the start of a passage.
func nodeName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxNodeName {
		return s
	}
	return string([]rune(s)[:maxNodeName]) + "…"
}
