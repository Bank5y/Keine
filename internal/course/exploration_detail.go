package course

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// ErrExplorationNotFound: no such exploration of the viewer's; somebody
// else's is not found either.
var ErrExplorationNotFound = errors.New("exploration not found")

// ExplorationDetail is one of the viewer's explorations whole: where it
// was made, its document, and its dialogue as a tree of nodes (ADR 0009).
type ExplorationDetail struct {
	ID       int64            `json:"id"`
	CourseID int64            `json:"courseId"`
	Quote    string           `json:"quote"`
	State    ExplorationState `json:"state"`
	// LessonTitle is the title of the lesson it was made on, "" once that
	// lesson is gone
	LessonTitle string `json:"lessonTitle"`
	Title       string `json:"title"`
	Intro       string `json:"intro"`
	Meaning     string `json:"meaning"`
	DocVersion  int    `json:"docVersion"`
	// the sections a sync fills, in order; empty until then (#138)
	Footnotes  []DocItem `json:"footnotes"`
	Extensions []DocItem `json:"extensions"`
	Expansions []DocItem `json:"expansions"`
	References []DocItem `json:"references"`
	Nodes      []Node    `json:"nodes"`
	CreatedAt  time.Time `json:"createdAt"`
}

// DocItem is one item of a section after the meaning.
type DocItem struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
}

// NodeKind is a node's type (ADR 0009); only the main node has no parent.
type NodeKind string

const (
	NodeMain      NodeKind = "main"
	NodeChild     NodeKind = "child"
	NodeBranch    NodeKind = "branch"
	NodeDivergent NodeKind = "divergent"
)

// Node is a line of turns in an exploration dialogue. Every node but the
// main one forks from a turn of its parent.
type Node struct {
	ID         int64     `json:"id"`
	Kind       NodeKind  `json:"kind"`
	Name       string    `json:"name"`
	ParentID   *int64    `json:"parentId"`
	ForkTurnID *int64    `json:"forkTurnId"`
	Turns      []Turn    `json:"turns"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Turn is a question and its reply, in Markdown. The main node's first
// turn is the analysis.
type Turn struct {
	ID        int64     `json:"id"`
	ParentID  *int64    `json:"parentId"`
	Question  Question  `json:"question"`
	Reply     string    `json:"reply"`
	Meta      TurnMeta  `json:"meta"`
	CreatedAt time.Time `json:"createdAt"`
}

// Question is what the reader asked, with what they attached to it.
type Question struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

// Attachment is a typed part of a question: "selection" (a passage of the
// lesson, or of a reply a node forked from) is the only kind so far.
type Attachment struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// TurnMeta is what the reader may see of a turn's metadata: not its usage.
type TurnMeta struct {
	Source   string            `json:"source"`
	Model    string            `json:"model"`
	Degraded []string          `json:"degraded,omitempty"`
	Sources  []json.RawMessage `json:"sources"`
	// Interrupted is why the reply stops short ("" when it is whole):
	// credits_exhausted, timeout or truncated
	Interrupted string `json:"interrupted,omitempty"`
}

// ExplorationDetail is one of the viewer's explorations, whatever its state.
func (s *Service) ExplorationDetail(ctx context.Context, viewer auth.User, id int64) (ExplorationDetail, error) {
	d := ExplorationDetail{Footnotes: []DocItem{}, Extensions: []DocItem{}, Expansions: []DocItem{}, References: []DocItem{}, Nodes: []Node{}}
	err := s.db.QueryRow(ctx, `SELECT e.id, e.course_id, e.quote, e.state, coalesce(l.title, ''), e.title, e.intro, e.meaning, e.doc_version, e.created_at
		FROM explorations e LEFT JOIN lessons l ON l.course_id = e.course_id AND l.key = e.lesson_key
		WHERE e.id = $1 AND e.user_id = $2`, id, viewer.ID).
		Scan(&d.ID, &d.CourseID, &d.Quote, &d.State, &d.LessonTitle, &d.Title, &d.Intro, &d.Meaning, &d.DocVersion, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrExplorationNotFound
	}
	if err != nil {
		return d, err
	}
	d.Nodes, err = dialogue(ctx, s.db, id)
	return d, err
}

// dialogue reads an exploration's nodes in the order they were made, each
// with its turns in order.
func dialogue(ctx context.Context, q querier, explorationID int64) ([]Node, error) {
	rows, err := q.Query(ctx, `SELECT id, kind, name, parent_id, fork_turn_id, created_at FROM exploration_nodes
		WHERE exploration_id = $1 ORDER BY id`, explorationID)
	if err != nil {
		return nil, err
	}
	nodes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Node, error) {
		n := Node{Turns: []Turn{}}
		err := r.Scan(&n.ID, &n.Kind, &n.Name, &n.ParentID, &n.ForkTurnID, &n.CreatedAt)
		return n, err
	})
	if err != nil {
		return nil, err
	}
	at := map[int64]int{}
	for i, n := range nodes {
		at[n.ID] = i
	}
	rows, err = q.Query(ctx, `SELECT t.id, t.node_id, t.parent_id, t.question, t.reply, t.meta, t.created_at
		FROM exploration_turns t JOIN exploration_nodes n ON n.id = t.node_id
		WHERE n.exploration_id = $1 ORDER BY t.id`, explorationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t Turn
		var node int64
		var question, meta []byte
		if err := rows.Scan(&t.ID, &node, &t.ParentID, &question, &t.Reply, &meta, &t.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(question, &t.Question); err != nil {
			return nil, err
		}
		if t.Question.Attachments == nil {
			t.Question.Attachments = []Attachment{}
		}
		if err := json.Unmarshal(meta, &t.Meta); err != nil {
			return nil, err
		}
		if t.Meta.Sources == nil {
			t.Meta.Sources = []json.RawMessage{}
		}
		nodes[at[node]].Turns = append(nodes[at[node]].Turns, t)
	}
	return nodes, rows.Err()
}

// DeleteExploration deletes one of the viewer's explorations with its
// whole dialogue.
func (s *Service) DeleteExploration(ctx context.Context, viewer auth.User, id int64) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM explorations WHERE id = $1 AND user_id = $2", id, viewer.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrExplorationNotFound
	}
	return nil
}
