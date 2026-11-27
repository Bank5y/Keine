package eval_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/db"
	"github.com/Bank5y/keine/internal/eval"
)

// buildIndex gives the course's version a built concept index: 任期 in
// lesson l01 (the selection's own) and 日志复制 in l02's section 复制.
func buildIndex(t *testing.T, dbURL string, id int64, version string) {
	t.Helper()
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	var index int64
	if err := pool.QueryRow(ctx, "INSERT INTO concept_indexes (course_id, files_dir, status, started_at) VALUES ($1, $2, 'built', now()) RETURNING id", id, version).Scan(&index); err != nil {
		t.Fatal(err)
	}
	section := anchor.Sections(lesson2)[1]
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO concept_index_lessons (index_id, lesson_key, text_hash) VALUES ($1, 'l01', 'h1'), ($1, 'l02', 'h2')", []any{index}},
		{"INSERT INTO concept_entries (index_id, lesson_key, position, name, summary) VALUES ($1, 'l01', 0, '任期', '选举的编号')", []any{index}},
		{"INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary) VALUES ($1, 'l02', 0, '日志复制', '{log replication}', $2, $3, '领导者把日志发给跟随者')", []any{index, section.ID, section.Title}},
		{"INSERT INTO concept_entries (index_id, lesson_key, position, name, aliases, section_id, section_title, summary) VALUES ($1, 'l02', 1, '领导者', '{}', $2, $3, '发起复制的节点')", []any{index, section.ID, section.Title}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
}

func linkCall(args string) assistant.Event {
	return assistant.Event{Kind: assistant.EventToolCall, ToolCall: &assistant.ToolCall{ID: "l1", Name: assistant.ToolLinkCourse, Arguments: args}}
}

func TestAFollowupCaseAsksItsTurnsAfterTheAnalysisAndLinksTheTurnANodeForksWith(t *testing.T) {
	dbURL, files, id, version := privateCourse(t)
	buildIndex(t, dbURL, id, version)
	before := counts(t, dbURL)
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	model := &script{steps: []assistant.Event{
		text(good),
		text("任期在每次选举时加一。"),
		// the child node's first turn names a concept of its own lesson and
		// one of l02, and links the section of 领导者, which it must not
		text("任期决定了日志复制时谁的日志更新，见 [论文](https://raft.github.io)。"),
		// the linking step names the candidate, and one it was not offered
		linkCall(`{"links":[{"section":"s1","note":"跟随者按任期核对日志"},{"section":"s4","note":"不存在"}]}`),
		text("本文档前面说过，任期只增不减。"),
	}}
	r := &eval.Runner{
		Courses: course.NewService(pool, files), Mermaid: noDiagrams{},
		Turn: func(context.Context) (assistant.Turn, error) {
			return assistant.Turn{Adapter: model, Model: "m-1", Rates: assistant.Rates{Input: 2, Output: 8}}, nil
		},
	}
	c := eval.Case{ID: "term-dialogue", Course: id, Version: version, Lesson: "l01", Quote: "任期", Prefix: "分成一个个",
		Turns: []eval.Turn{
			{Ask: "任期什么时候变？"},
			{Fork: "child", Ask: "任期和日志有什么关系？", MustInclude: []string{"日志"}, MustNotLink: []string{"领导者"}},
			{Ask: "任期会变小吗？"},
		}}
	res, err := r.Run(context.Background(), c, 1)
	if err != nil {
		t.Fatal(err)
	}

	if after := counts(t, dbURL); !equal(before, after) {
		t.Fatalf("evaluating wrote to the database: before %v, after %v", before, after)
	}
	if len(res.Turns) != 3 || len(model.asked) != 5 {
		t.Fatalf("turns %+v, %d calls", res.Turns, len(model.asked))
	}
	main, fork, last := res.Turns[0], res.Turns[1], res.Turns[2]
	if main.Reply != "任期在每次选举时加一。" || main.Linking != nil || main.Stats.Usage.Input == nil {
		t.Fatalf("main node's turn: %+v", main)
	}
	// the turn a node forks with is asked with the node's note, and its
	// answer linked: the selection's own lesson is no candidate
	if q := lastUser(model.asked[2]); !strings.HasPrefix(q, assistant.NodeNote("child")) || !strings.Contains(q, "任期和日志有什么关系？") {
		t.Fatalf("the fork's question: %q", q)
	}
	if l := lastUser(model.asked[3]); !strings.Contains(l, "跟随者按任期和索引核对日志") || strings.Contains(l, "选举的编号") {
		t.Fatalf("the linking step's message: %q", l)
	}
	lk := fork.Linking
	if lk == nil || lk.Index != course.ConceptIndexBuilt || len(lk.Candidates) != 1 || len(lk.Chosen) != 1 {
		t.Fatalf("linking: %+v", lk)
	}
	if got := lk.Candidates[0]; got.Label != "日志复制" || got.Lesson != "02｜日志复制" || got.Section != "复制" {
		t.Fatalf("candidate: %+v", got)
	}
	if got := lk.Chosen[0]; got.Label != "日志复制" || got.Note != "跟随者按任期核对日志" {
		t.Fatalf("chosen: %+v", got)
	}
	// a later turn reads the fork's reply with what it linked
	if !strings.Contains(joined(model.asked[4]), "课程里的相关内容：\n- 讲「02｜日志复制」 › 小节「复制」：跟随者按任期核对日志") {
		t.Fatalf("history of the last turn: %q", joined(model.asked[4]))
	}

	broken := map[int][]string{}
	for _, f := range res.Failures {
		broken[f.Turn] = append(broken[f.Turn], f.Rule)
	}
	if len(broken[0]) != 0 || len(broken[1]) != 0 {
		t.Fatalf("the analysis and the main node's turn broke rules: %+v", res.Failures)
	}
	if got := strings.Join(broken[2], ","); got != eval.RuleReply+","+eval.RuleLinkCourse+","+eval.RuleMustNotLink {
		t.Fatalf("the fork broke %s: %+v", got, res.Failures)
	}
	if got := strings.Join(broken[3], ","); got != eval.RuleNarration {
		t.Fatalf("the last turn broke %s: %+v", got, res.Failures)
	}
	if last.Linking != nil {
		t.Fatalf("a turn after the fork is linked: %+v", last.Linking)
	}
}

func TestAForkSharingNoWordsWithACourseWithoutAnIndexIsNotLinked(t *testing.T) {
	dbURL, files, id, version := privateCourse(t)
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	model := &script{steps: []assistant.Event{text(good), text("Paxos 另说。")}}
	r := &eval.Runner{
		Courses: course.NewService(pool, files), Mermaid: noDiagrams{},
		Turn: func(context.Context) (assistant.Turn, error) {
			return assistant.Turn{Adapter: model, Model: "m-1"}, nil
		},
	}
	c := eval.Case{ID: "no-index", Course: id, Version: version, Lesson: "l01", Quote: "任期", Prefix: "分成一个个",
		Turns: []eval.Turn{{Fork: "branch", Ask: "还有哪些相关概念？"}}}
	res, err := r.Run(context.Background(), c, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hard() || len(res.Turns) != 1 || len(model.asked) != 2 {
		t.Fatalf("%+v, %d calls", res, len(model.asked))
	}
	if lk := res.Turns[0].Linking; lk == nil || lk.Index != course.ConceptIndexNone || len(lk.Candidates) != 0 {
		t.Fatalf("linking: %+v", lk)
	}
}

func lastUser(req assistant.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == assistant.RoleUser {
			return req.Messages[i].Content
		}
	}
	return ""
}

func joined(req assistant.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content + "\n")
	}
	return b.String()
}

func TestAReplyCitingANumberNoSearchGaveFails(t *testing.T) {
	dbURL, files, id, version := privateCourse(t)
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// no web: nothing was found, so [1] cites nothing
	model := &script{steps: []assistant.Event{text(good), text("Raft 的论文见 [1]。")}}
	r := &eval.Runner{
		Courses: course.NewService(pool, files), Mermaid: noDiagrams{},
		Turn: func(context.Context) (assistant.Turn, error) {
			return assistant.Turn{Adapter: model, Model: "m-1"}, nil
		},
	}
	c := eval.Case{ID: "cites-nothing", Course: id, Version: version, Lesson: "l01", Quote: "任期", Prefix: "分成一个个",
		Turns: []eval.Turn{{Ask: "找几篇 Raft 的论文并给出链接"}}}
	res, err := r.Run(context.Background(), c, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 || res.Failures[0].Rule != eval.RuleCitation || res.Failures[0].Turn != 1 {
		t.Fatalf("failures %+v", res.Failures)
	}
}
