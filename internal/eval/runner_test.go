package eval_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/db"
	"github.com/Bank5y/keine/internal/eval"
	"github.com/Bank5y/keine/internal/server"
	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/testutil"
)

const lesson = "# 01｜任期\n\nRaft 把时间分成一个个任期，每个任期开始时进行选举。\n\n## 选举\n\n候选人在任期内拉票。\n"

// privateCourse is a course alice uploaded and never published, in a
// fresh database: its id and its version.
func privateCourse(t *testing.T) (dbURL string, files storage.Store, id int64, version string) {
	t.Helper()
	dbURL, files = testutil.NewDatabase(t), storage.NewLocal(t.TempDir())
	s, err := server.New(context.Background(), server.Options{DatabaseURL: dbURL, Storage: files})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); s.Close() })
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	post := func(path string, body any) {
		b, _ := json.Marshal(body)
		res, err := c.Post(ts.URL+path, "application/json", bytes.NewReader(b))
		if err != nil || res.StatusCode >= 300 {
			t.Fatalf("POST %s: %v %v", path, err, res)
		}
		res.Body.Close()
	}
	post("/api/users", map[string]any{"username": "alice", "password": "alice-password"})
	post("/api/session", map[string]any{"provider": "password", "username": "alice", "password": "alice-password"})

	manifest, _ := json.Marshal(map[string]any{"version": 1, "title": "分布式系统", "type": "column",
		"chapters": []any{map[string]any{"id": "c1", "title": "共识", "lessons": []any{map[string]any{"id": "l01", "title": "01｜任期", "file": "01.md"}}}}})
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	for name, body := range map[string]string{"course.json": string(manifest), "01.md": lesson} {
		f, _ := zw.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = zw.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("package", "course.zip")
	_, _ = fw.Write(zbuf.Bytes())
	_ = mw.Close()
	res, err := c.Post(ts.URL+"/api/courses", mw.FormDataContentType(), &body)
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %v %v", err, res)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	_ = json.Unmarshal(raw, &created)

	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.QueryRow(context.Background(), "SELECT files_dir FROM courses WHERE id = $1", created.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return dbURL, files, created.ID, version
}

// script answers each call with the next text, reading the lesson first
// when asked to.
type script struct {
	mu    sync.Mutex
	steps []assistant.Event
}

func (s *script) Stream(_ context.Context, _ assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	s.mu.Lock()
	e := s.steps[0]
	s.steps = s.steps[1:]
	s.mu.Unlock()
	if err := emit(e); err != nil {
		return assistant.Result{}, err
	}
	if e.Kind == assistant.EventToolCall {
		return assistant.Result{Finish: assistant.FinishToolCalls, Usage: assistant.Usage{Input: new(int64(1000)), Output: new(int64(20))}}, nil
	}
	return assistant.Result{Finish: assistant.FinishStop, Usage: assistant.Usage{Input: new(int64(1200)), Output: new(int64(300))}}, nil
}

func text(s string) assistant.Event { return assistant.Event{Kind: assistant.EventText, Text: s} }

const good = "<exp_title>任期（term）</exp_title>\n<exp_concept>Raft 用来给时间分段的递增编号。</exp_concept>\n<exp_explanation>\n在这一讲里，**任期**是每次选举开始时加一的编号。\n</exp_explanation>"

func counts(t *testing.T, dbURL string) map[string]int {
	t.Helper()
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	out := map[string]int{}
	for _, table := range []string{"explorations", "exploration_turns", "usage_records", "annotations", "lesson_progress"} {
		var n int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

func TestEvaluatingReadsTheCourseAndWritesNothing(t *testing.T) {
	dbURL, files, id, version := privateCourse(t)
	before := counts(t, dbURL)
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	heading := strings.Replace(good, "在这一讲里", "## 任期\n\n在这一讲里", 1)
	model := &script{steps: []assistant.Event{
		// the first case reads its own lesson again, then answers
		{Kind: assistant.EventToolCall, ToolCall: &assistant.ToolCall{ID: "c1", Name: "read_lesson", Arguments: `{"lesson":"l01"}`}},
		text(good),
		// the second first answers outside the tags, which nobody saw, so
		// it is asked again, and answers with a heading
		text("任期就是 term。"), text(heading),
	}}
	r := &eval.Runner{
		Courses: course.NewService(pool, files), Mermaid: noDiagrams{},
		Turn: func(context.Context) (assistant.Turn, error) {
			return assistant.Turn{Adapter: model, Model: "m-1", Rates: assistant.Rates{Input: 2, Output: 8}}, nil
		},
	}
	cases := []eval.Case{
		{ID: "term", Course: id, Version: version, Lesson: "l01", Quote: "任期", Prefix: "分成一个个", MustInclude: []string{"选举"}},
		{ID: "heading", Course: id, Version: version, Lesson: "l01", Quote: "候选人在任期内拉票"},
		{ID: "stale", Course: id, Version: "v-old", Lesson: "l01", Quote: "任期"},
	}
	var results []eval.Result
	for _, c := range cases {
		res, err := r.Run(context.Background(), c, 1)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, res)
	}

	if after := counts(t, dbURL); !equal(before, after) {
		t.Fatalf("evaluating wrote to the database: before %v, after %v", before, after)
	}
	term := results[0]
	if term.Hard() || term.Analysis.Title != "任期（term）" || !term.Stats.Reread || term.Stats.Steps != 1 || term.Stats.ToolCalls != 1 || term.Stats.Remedies != 0 {
		t.Fatalf("term: %+v", term)
	}
	if *term.Stats.Usage.Input != 2200 || term.Stats.Credits <= 0 || term.Stats.FirstTextMs < 0 || term.Stats.LessonText != "full" {
		t.Fatalf("term stats: %+v", term.Stats)
	}
	h := results[1]
	if h.Error != assistant.CodeMalformedOutput || h.Stats.Remedies != 1 || !h.Hard() || h.Failures[0].Rule != eval.RuleServer {
		t.Fatalf("heading: %+v", h)
	}
	if s := results[2]; len(s.Failures) != 1 || s.Failures[0].Rule != eval.RuleCase {
		t.Fatalf("stale: %+v", s)
	}
}

type noDiagrams struct{}

func (noDiagrams) Parse(d []string) ([]error, error) { return make([]error, len(d)), nil }

func equal(a, b map[string]int) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return len(a) == len(b)
}
