package course

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
)

// Errors of the exploration dialogue (ADR 0009).
var (
	ErrNodeNotFound = errors.New("node not found")
	ErrTurnNotFound = errors.New("turn not found")
	// ErrNotLastTurn: within a node turns follow one another, so a turn
	// goes only after the node's last one; to go elsewhere, open a node.
	ErrNotLastTurn = errors.New("not the node's last turn")
	// ErrMainNode: the main node is the dialogue's root and stays.
	ErrMainNode = errors.New("the main node cannot be deleted")
	// ErrNodeLimit: an exploration holds MaxNodes nodes, the main one too.
	ErrNodeLimit = errors.New("too many nodes")
	// ErrNotRetriable: only a turn that failed or stopped short is tried
	// again, and only while nothing follows it.
	ErrNotRetriable = errors.New("turn cannot be tried again")
)

// MaxNodes is how many nodes an exploration holds, the main one too (#118).
const MaxNodes = 50

// limits on a question, in characters
const (
	maxQuestion = 4000
	maxFragment = 2000
)

// Node returns the node of id in d.
func (d ExplorationDetail) Node(id int64) (Node, bool) {
	for _, n := range d.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// Chain is what a turn asked after turn `upTo` sees of the dialogue: the
// turns from the main node's first one (the analysis) along the chain of
// nodes that leads to upTo's node, each node cut at the turn its child
// forked from, and upTo's node up to upTo (ADR 0009). Later turns of a
// parent and sibling nodes are not in it.
func (d ExplorationDetail) Chain(upTo int64) ([]Turn, bool) {
	var at *Node
	cut := -1
	for i := range d.Nodes {
		for j, t := range d.Nodes[i].Turns {
			if t.ID == upTo {
				at, cut = &d.Nodes[i], j
			}
		}
	}
	if at == nil {
		return nil, false
	}
	var chain []Turn
	n := *at
	for {
		chain = append(append([]Turn{}, n.Turns[:cut+1]...), chain...)
		if n.ParentID == nil {
			return chain, true
		}
		p, ok := d.Node(*n.ParentID)
		if !ok {
			return nil, false
		}
		cut = -1
		for j, t := range p.Turns {
			if n.ForkTurnID != nil && t.ID == *n.ForkTurnID {
				cut = j
			}
		}
		if cut < 0 {
			return nil, false
		}
		n = p
	}
}

// AfterLast tells whether a question can go after turn `after`: it is the
// last of its node (ErrNotLastTurn when not, ErrTurnNotFound when there is
// no such turn).
func (d ExplorationDetail) AfterLast(after int64) error {
	for _, n := range d.Nodes {
		for i, t := range n.Turns {
			if t.ID == after {
				if i != len(n.Turns)-1 {
					return ErrNotLastTurn
				}
				return nil
			}
		}
	}
	return ErrTurnNotFound
}

// Snapshot is the request an exploration's analysis ran on, as pinned.
func (s *Service) Snapshot(ctx context.Context, viewer auth.User, explorationID int64) (json.RawMessage, error) {
	var b []byte
	err := s.db.QueryRow(ctx, "SELECT snapshot FROM explorations WHERE id = $1 AND user_id = $2", explorationID, viewer.ID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExplorationNotFound
	}
	return b, err
}

// NewTurn is a turn to keep: what was asked, the reply as far as it got,
// and the turn's metadata (stored as JSON).
type NewTurn struct {
	Question Question
	Reply    string
	Meta     any
}

func (q Question) valid() bool {
	if utf8.RuneCountInString(q.Text) > maxQuestion || len(q.Attachments) > 1 {
		return false
	}
	for _, a := range q.Attachments {
		if a.Kind != AttachSelection || strings.TrimSpace(a.Text) == "" || utf8.RuneCountInString(a.Text) > maxFragment {
			return false
		}
	}
	return strings.TrimSpace(q.Text) != "" || len(q.Attachments) > 0
}

// ValidQuestion tells whether a question can be asked: some text or a
// selection, within the limits.
func ValidQuestion(q Question) bool { return q.valid() }

// AddTurn keeps a turn after `after`, the last turn of its node, in one of
// the viewer's explorations.
func (s *Service) AddTurn(ctx context.Context, viewer auth.User, explorationID, after int64, in NewTurn) (Turn, error) {
	var t Turn
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var node int64
		err := tx.QueryRow(ctx, `SELECT t.node_id FROM exploration_turns t
			JOIN exploration_nodes n ON n.id = t.node_id JOIN explorations e ON e.id = n.exploration_id
			WHERE t.id = $1 AND e.id = $2 AND e.user_id = $3 FOR UPDATE OF n`, after, explorationID, viewer.ID).Scan(&node)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTurnNotFound
		}
		if err != nil {
			return err
		}
		var later bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM exploration_turns WHERE parent_id = $1)", after).Scan(&later); err != nil {
			return err
		}
		if later {
			return ErrNotLastTurn
		}
		t, err = insertTurn(ctx, tx, node, &after, in)
		return err
	})
	return t, err
}

func insertTurn(ctx context.Context, tx pgx.Tx, node int64, parent *int64, in NewTurn) (Turn, error) {
	question, err := json.Marshal(in.Question)
	if err != nil {
		return Turn{}, err
	}
	meta, err := json.Marshal(in.Meta)
	if err != nil {
		return Turn{}, err
	}
	t := Turn{ParentID: parent, Question: in.Question, Reply: in.Reply}
	if err := tx.QueryRow(ctx, "INSERT INTO exploration_turns (node_id, parent_id, question, reply, meta) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at",
		node, parent, question, in.Reply, meta).Scan(&t.ID, &t.CreatedAt); err != nil {
		return Turn{}, err
	}
	return t, t.readMeta(meta)
}

// RetryTarget is a turn tried again: where it sits and what it asked.
type RetryTarget struct {
	Turn Turn
	Node Node
}

// Retriable is a turn of the viewer's that failed or stopped short, which
// nothing follows: no later turn, no node forked from it.
func (d ExplorationDetail) Retriable(turnID int64) (RetryTarget, error) {
	for _, n := range d.Nodes {
		for i, t := range n.Turns {
			if t.ID != turnID {
				continue
			}
			if t.Meta.Interrupted == "" || i != len(n.Turns)-1 {
				return RetryTarget{}, ErrNotRetriable
			}
			for _, m := range d.Nodes {
				if m.ForkTurnID != nil && *m.ForkTurnID == turnID {
					return RetryTarget{}, ErrNotRetriable
				}
			}
			return RetryTarget{Turn: t, Node: n}, nil
		}
	}
	return RetryTarget{}, ErrTurnNotFound
}

// ReplaceTurn writes a turn tried again over the one that failed, in
// place: same question, same place in its node (#136). Whether it may is
// decided again under the node's lock, as the run took a while: the turn
// must still have stopped short with nothing after it.
func (s *Service) ReplaceTurn(ctx context.Context, viewer auth.User, explorationID, turnID int64, reply string, meta any) (Turn, error) {
	b, err := json.Marshal(meta)
	if err != nil {
		return Turn{}, err
	}
	var t Turn
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var was Interruption
		err := tx.QueryRow(ctx, `SELECT coalesce(t.meta->>'interrupted', '') FROM exploration_turns t
			JOIN exploration_nodes n ON n.id = t.node_id JOIN explorations e ON e.id = n.exploration_id
			WHERE t.id = $1 AND e.id = $2 AND e.user_id = $3 FOR UPDATE OF n`, turnID, explorationID, viewer.ID).Scan(&was)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTurnNotFound
		}
		if err != nil {
			return err
		}
		var followed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exploration_turns WHERE parent_id = $1)
			OR EXISTS (SELECT 1 FROM exploration_nodes WHERE fork_turn_id = $1)`, turnID).Scan(&followed); err != nil {
			return err
		}
		if was == "" || followed {
			return ErrNotRetriable
		}
		var question, stored []byte
		if err := tx.QueryRow(ctx, `UPDATE exploration_turns SET reply = $2, meta = $3 WHERE id = $1
			RETURNING id, parent_id, question, reply, meta, created_at`, turnID, reply, b).
			Scan(&t.ID, &t.ParentID, &question, &t.Reply, &stored, &t.CreatedAt); err != nil {
			return err
		}
		if err := json.Unmarshal(question, &t.Question); err != nil {
			return err
		}
		return t.readMeta(stored)
	})
	return t, err
}

// NodeName names a node: after the passage it was opened on, or else the
// start of its first question; no model is asked (#137).
func NodeName(q Question) string {
	if s := q.Selections(); len(s) > 0 {
		return nodeName(s[0])
	}
	return nodeName(q.Text)
}

// OpenNode opens a node of kind forking from turn `fork` of one of the
// viewer's explorations, and keeps its first turn. The kind is the
// reader's and stays.
func (s *Service) OpenNode(ctx context.Context, viewer auth.User, explorationID, fork int64, kind NodeKind, first NewTurn) (Node, error) {
	if !kind.Openable() {
		return Node{}, ErrBadInput
	}
	var n Node
	err := s.tx(ctx, func(tx pgx.Tx) error {
		// one change of the exploration's nodes at a time, so the limit holds
		if _, err := tx.Exec(ctx, "SELECT 1 FROM explorations WHERE id = $1 AND user_id = $2 FOR UPDATE", explorationID, viewer.ID); err != nil {
			return err
		}
		var parent int64
		err := tx.QueryRow(ctx, `SELECT n.id FROM exploration_turns t JOIN exploration_nodes n ON n.id = t.node_id
			JOIN explorations e ON e.id = n.exploration_id WHERE t.id = $1 AND e.id = $2 AND e.user_id = $3`, fork, explorationID, viewer.ID).Scan(&parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTurnNotFound
		}
		if err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM exploration_nodes WHERE exploration_id = $1", explorationID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxNodes {
			return ErrNodeLimit
		}
		n = Node{Kind: kind, Name: NodeName(first.Question), ParentID: &parent, ForkTurnID: &fork}
		if err := tx.QueryRow(ctx, `INSERT INTO exploration_nodes (exploration_id, kind, parent_id, fork_turn_id, name) VALUES ($1, $2, $3, $4, $5)
			RETURNING id, created_at`, explorationID, kind, parent, fork, n.Name).Scan(&n.ID, &n.CreatedAt); err != nil {
			return err
		}
		t, err := insertTurn(ctx, tx, n.ID, nil, first)
		n.Turns = []Turn{t}
		return err
	})
	return n, err
}

// DeleteNode deletes a node of one of the viewer's explorations with the
// nodes under it; the main node stays.
func (s *Service) DeleteNode(ctx context.Context, viewer auth.User, explorationID, nodeID int64) error {
	var kind NodeKind
	err := s.db.QueryRow(ctx, `SELECT n.kind FROM exploration_nodes n JOIN explorations e ON e.id = n.exploration_id
		WHERE n.id = $1 AND e.id = $2 AND e.user_id = $3`, nodeID, explorationID, viewer.ID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNodeNotFound
	}
	if err != nil {
		return err
	}
	if kind == NodeMain {
		return ErrMainNode
	}
	// the nodes under it go with it (their parent and fork turn cascade)
	_, err = s.db.Exec(ctx, "DELETE FROM exploration_nodes WHERE id = $1", nodeID)
	return err
}
