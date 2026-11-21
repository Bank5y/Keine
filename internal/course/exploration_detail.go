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
	// LessonKey is the key of the lesson it was made on
	LessonKey string `json:"-"`
	// the document (ADR 0012): title, concept and concept explanation
	Title       string    `json:"title"`
	Concept     string    `json:"concept"`
	Explanation string    `json:"explanation"`
	Nodes       []Node    `json:"nodes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// NodeKind is a node's type (ADR 0009); only the main node has no parent.
type NodeKind string

const (
	NodeMain      NodeKind = "main"
	NodeChild     NodeKind = "child"
	NodeBranch    NodeKind = "branch"
	NodeDivergent NodeKind = "divergent"
)

// Openable tells whether a reader can open a node of this kind: any but
// the main one, which pinning makes.
func (k NodeKind) Openable() bool { return k == NodeChild || k == NodeBranch || k == NodeDivergent }

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
	// CourseLinks are what the turn a node forks with linked of the course
	// (ADR 0011), kept in its metadata
	CourseLinks []CourseLink `json:"courseLinks"`
	// NodeKind is the kind of the turn's node, so a chain of turns tells
	// which turn forked which kind of node
	NodeKind NodeKind `json:"-"`
}

// Question is what the reader asked, with what they attached to it.
type Question struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

// Attachment is a typed part of a question (ADR 0009).
type Attachment struct {
	Kind AttachmentKind `json:"kind"`
	Text string         `json:"text"`
}

// AttachmentKind is what an attachment is.
type AttachmentKind string

// AttachSelection is a passage the reader selected: of the lesson, or of
// the reply a node forked from.
const AttachSelection AttachmentKind = "selection"

// Selections are the passages a question attaches.
func (q Question) Selections() []string {
	var out []string
	for _, a := range q.Attachments {
		if a.Kind == AttachSelection {
			out = append(out, a.Text)
		}
	}
	return out
}

// Interruption is why a turn's reply stops short (#136); "" when it is
// whole.
type Interruption string

const (
	// InterruptedCredits: the reader's credits ran out during the run
	InterruptedCredits Interruption = "credits_exhausted"
	InterruptedTimeout Interruption = "timeout"
	// InterruptedTruncated: the reply reached the output limit
	InterruptedTruncated Interruption = "truncated"
	// InterruptedFailed: the run ended in an error, before or after some
	// of the reply
	InterruptedFailed Interruption = "failed"
)

// TurnMeta is what the reader may see of a turn's metadata: not its usage.
type TurnMeta struct {
	Source      string            `json:"source"`
	Model       string            `json:"model"`
	Degraded    []string          `json:"degraded,omitempty"`
	Sources     []json.RawMessage `json:"sources"`
	Interrupted Interruption      `json:"interrupted,omitempty"`
}

// CourseLink is a concept of another lesson that a turn's answer linked
// (ADR 0011): the index entry's concept and place, and a sentence on how
// it relates to the answer.
type CourseLink struct {
	Concept string        `json:"concept"`
	Note    string        `json:"note"`
	Lesson  EntryLesson   `json:"lesson"`
	Section *EntrySection `json:"section"`
}

// readMeta reads a turn's stored metadata: what the reader sees of it, and
// its course links.
func (t *Turn) readMeta(raw []byte) error {
	var links struct {
		CourseLinks []CourseLink `json:"courseLinks"`
	}
	if err := json.Unmarshal(raw, &t.Meta); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		return err
	}
	if t.Meta.Sources == nil {
		t.Meta.Sources = []json.RawMessage{}
	}
	t.CourseLinks = links.CourseLinks
	if t.CourseLinks == nil {
		t.CourseLinks = []CourseLink{}
	}
	return nil
}

// ExplorationDetail is one of the viewer's explorations, whatever its state.
func (s *Service) ExplorationDetail(ctx context.Context, viewer auth.User, id int64) (ExplorationDetail, error) {
	d := ExplorationDetail{Nodes: []Node{}}
	err := s.db.QueryRow(ctx, `SELECT e.id, e.course_id, e.quote, e.state, coalesce(l.title, ''), e.lesson_key, e.title, e.concept, e.explanation, e.created_at
		FROM explorations e LEFT JOIN lessons l ON l.course_id = e.course_id AND l.key = e.lesson_key
		WHERE e.id = $1 AND e.user_id = $2`, id, viewer.ID).
		Scan(&d.ID, &d.CourseID, &d.Quote, &d.State, &d.LessonTitle, &d.LessonKey, &d.Title, &d.Concept, &d.Explanation, &d.CreatedAt)
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
		if err := t.readMeta(meta); err != nil {
			return nil, err
		}
		t.NodeKind = nodes[at[node]].Kind
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
