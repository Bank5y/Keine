package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/assistant/search"
)

// service is a fake search service answering every request with status
// and body, and keeping what it was sent.
type service struct {
	status int
	body   string
	got    []map[string]any
	paths  []string
	auth   []string
}

func (s *service) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		s.got = append(s.got, m)
		s.paths = append(s.paths, r.URL.Path)
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func failureOf(t *testing.T, err error) *assistant.ToolError {
	t.Helper()
	var te *assistant.ToolError
	if !errors.As(err, &te) {
		t.Fatalf("error %v is not a tool error", err)
	}
	return te
}

func TestTavilySearchNormalizesResults(t *testing.T) {
	s := &service{status: 200, body: `{"query":"raft term","images":[],"results":[
		{"title":"Raft","url":"https://raft.github.io/","content":"Raft is a consensus algorithm","score":0.9},
		{"title":"Paper","url":"https://example.com/raft.pdf","content":"In Search of an Understandable Consensus Algorithm","score":0.8}
	],"usage":{"credits":1}}`}
	tv := search.NewTavily(s.start(t), "tvly-key")
	hits, billed, err := tv.Search(context.Background(), "raft term", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []assistant.SearchHit{
		{Title: "Raft", URL: "https://raft.github.io/", Summary: "Raft is a consensus algorithm"},
		{Title: "Paper", URL: "https://example.com/raft.pdf", Summary: "In Search of an Understandable Consensus Algorithm"},
	}
	if len(hits) != 2 || hits[0] != want[0] || hits[1] != want[1] {
		t.Fatalf("hits = %+v", hits)
	}
	if !billed {
		t.Error("a 200 search is billed by Tavily")
	}
	if s.paths[0] != "/search" || s.auth[0] != "Bearer tvly-key" {
		t.Errorf("request to %s with %q", s.paths[0], s.auth[0])
	}
	if s.got[0]["query"] != "raft term" || s.got[0]["max_results"] != float64(5) || s.got[0]["search_depth"] != "basic" {
		t.Errorf("body = %v", s.got[0])
	}
}

func TestTavilyBillsAnEmptySearch(t *testing.T) {
	s := &service{status: 200, body: `{"query":"q","results":[]}`}
	hits, billed, err := search.NewTavily(s.start(t), "k").Search(context.Background(), "q", 5)
	if err != nil || len(hits) != 0 || !billed {
		t.Fatalf("hits %v billed %v err %v", hits, billed, err)
	}
}

func TestDeepSeekSearchNormalizesResults(t *testing.T) {
	s := &service{status: 200, body: `{"results":[
		{"rerank_score":2.1,"title":"Raft","url":"https://raft.github.io/","snippet":"short","summary":"long summary of raft","content":"","authority":3},
		{"rerank_score":1.1,"title":"No summary","url":"https://example.com/","snippet":"only a snippet","summary":"","content":""}
	]}`}
	ds := search.NewDeepSeek(s.start(t), "sk-ds")
	hits, billed, err := ds.Search(context.Background(), "raft", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Summary != "long summary of raft" || hits[1].Summary != "only a snippet" || hits[0].URL != "https://raft.github.io/" {
		t.Fatalf("hits = %+v", hits)
	}
	if !billed {
		t.Error("a search with results is billed")
	}
	if s.paths[0] != "/v1/tools/search" || s.auth[0] != "Bearer sk-ds" {
		t.Errorf("request to %s with %q", s.paths[0], s.auth[0])
	}
	if s.got[0]["query"] != "raft" || s.got[0]["count"] != float64(5) || s.got[0]["need_content"] != false {
		t.Errorf("body = %v", s.got[0])
	}
}

func TestDeepSeekDoesNotBillAnEmptySearch(t *testing.T) {
	s := &service{status: 200, body: `{"results":[]}`}
	hits, billed, err := search.NewDeepSeek(s.start(t), "k").Search(context.Background(), "q", 5)
	if err != nil || len(hits) != 0 || billed {
		t.Fatalf("hits %v billed %v err %v", hits, billed, err)
	}
}

func TestDeepSeekFetchReadsAPage(t *testing.T) {
	s := &service{status: 200, body: `{"title":"Raft","content":"# Raft\n\nConsensus."}`}
	p, billed, err := search.NewDeepSeek(s.start(t), "k").Fetch(context.Background(), "https://raft.github.io/")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Raft" || p.Content != "# Raft\n\nConsensus." || !billed {
		t.Fatalf("page %+v billed %v", p, billed)
	}
	if s.paths[0] != "/v1/tools/fetch" || s.got[0]["url"] != "https://raft.github.io/" {
		t.Errorf("request to %s: %v", s.paths[0], s.got[0])
	}
}

func TestDeepSeekDoesNotBillAnEmptyPage(t *testing.T) {
	s := &service{status: 200, body: `{"title":"","content":""}`}
	_, billed, err := search.NewDeepSeek(s.start(t), "k").Fetch(context.Background(), "https://example.com/")
	if err != nil || billed {
		t.Fatalf("billed %v err %v", billed, err)
	}
}

// failures are how each service's error statuses are classified (#117).
func TestFailuresAreClassified(t *testing.T) {
	cases := []struct {
		name    string
		call    func(base string) error
		status  int
		failure assistant.ToolFailure
		reason  assistant.Category
	}{
		{"tavily 401", tavilySearch, 401, assistant.FailureAccount, assistant.ErrAuth},
		{"tavily 432", tavilySearch, 432, assistant.FailureAccount, assistant.ErrQuotaExhausted},
		{"tavily 433", tavilySearch, 433, assistant.FailureAccount, assistant.ErrQuotaExhausted},
		{"tavily 429", tavilySearch, 429, assistant.FailureTransient, assistant.ErrRateLimited},
		{"tavily 500", tavilySearch, 500, assistant.FailureTransient, assistant.ErrUnavailable},
		{"tavily 400", tavilySearch, 400, assistant.FailureInvalid, assistant.ErrInvalidRequest},
		{"deepseek 402", deepseekSearch, 402, assistant.FailureAccount, assistant.ErrQuotaExhausted},
		{"deepseek 401", deepseekSearch, 401, assistant.FailureAccount, assistant.ErrAuth},
		{"deepseek 429", deepseekSearch, 429, assistant.FailureTransient, assistant.ErrRateLimited},
		{"deepseek 503", deepseekSearch, 503, assistant.FailureTransient, assistant.ErrUnavailable},
		{"deepseek 504", deepseekSearch, 504, assistant.FailureTransient, assistant.ErrTimeout},
		{"deepseek 400", deepseekSearch, 400, assistant.FailureInvalid, assistant.ErrInvalidRequest},
		// a fetch refused by policy or with no readable text is about the
		// page, not the account
		{"deepseek fetch 403", deepseekFetch, 403, assistant.FailureInvalid, assistant.ErrNotFound},
		{"deepseek fetch 404", deepseekFetch, 404, assistant.FailureInvalid, assistant.ErrNotFound},
		{"deepseek fetch 402", deepseekFetch, 402, assistant.FailureAccount, assistant.ErrQuotaExhausted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &service{status: c.status, body: `{"error":{"message":"no","type":"x"}}`}
			te := failureOf(t, c.call(s.start(t)))
			if te.Failure != c.failure || te.Reason != c.reason {
				t.Errorf("failure %v reason %s, want %v %s", te.Failure, te.Reason, c.failure, c.reason)
			}
		})
	}
}

func TestUnreachableServiceIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	te := failureOf(t, tavilySearch(base))
	if te.Failure != assistant.FailureTransient || te.Reason != assistant.ErrUnavailable {
		t.Errorf("failure %v reason %s", te.Failure, te.Reason)
	}
}

func TestUnreadableAnswerIsTransient(t *testing.T) {
	s := &service{status: 200, body: `<html>`}
	te := failureOf(t, deepseekSearch(s.start(t)))
	if te.Failure != assistant.FailureTransient {
		t.Errorf("failure %v reason %s", te.Failure, te.Reason)
	}
}

func tavilySearch(base string) error {
	_, _, err := search.NewTavily(base, "k").Search(context.Background(), "q", 5)
	return err
}

func deepseekSearch(base string) error {
	_, _, err := search.NewDeepSeek(base, "k").Search(context.Background(), "q", 5)
	return err
}

func deepseekFetch(base string) error {
	_, _, err := search.NewDeepSeek(base, "k").Fetch(context.Background(), "https://example.com/")
	return err
}
