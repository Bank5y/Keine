// Package search is the search service adapters behind Keine's web_search
// and web_fetch tools (ADR 0008, the Keine tool route): Tavily, and
// DeepSeek Search / Fetch, which takes the DeepSeek model's own key. Each
// normalizes its results, classifies its failures the way the assistant
// loop needs them (#117), and tells whether the service bills a request.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Bank5y/keine/internal/assistant"
)

// Default addresses of the services.
const (
	TavilyURL   = "https://api.tavily.com"
	DeepSeekURL = "https://api.deepseek.com"
)

// most of an answer read; anything past it is not a search result
const maxAnswer = 8 << 20

// Tavily searches with Tavily. It does not read pages.
type Tavily struct{ client }

func NewTavily(baseURL, key string) *Tavily { return &Tavily{newClient(baseURL, key)} }

// Search runs a basic search (one credit). Tavily bills every search it
// answers with 200, results or not.
func (t *Tavily) Search(ctx context.Context, query string, count int) ([]assistant.SearchHit, bool, error) {
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	err := t.post(ctx, "/search", map[string]any{"query": query, "max_results": count, "search_depth": "basic"}, &out, tavilyFailure)
	if err != nil {
		return nil, false, err
	}
	hits := make([]assistant.SearchHit, 0, len(out.Results))
	for _, r := range out.Results {
		if r.URL != "" {
			hits = append(hits, assistant.SearchHit{Title: r.Title, URL: r.URL, Summary: r.Content})
		}
	}
	return hits, true, nil
}

// tavilyFailure adds Tavily's own account statuses: 432 (key or plan
// limit) and 433 (pay-as-you-go limit).
func tavilyFailure(status int) (assistant.ToolFailure, assistant.Category, bool) {
	if status == 432 || status == 433 {
		return assistant.FailureAccount, assistant.ErrQuotaExhausted, true
	}
	return 0, "", false
}

// DeepSeek searches and reads pages with DeepSeek Search / Fetch.
type DeepSeek struct{ client }

func NewDeepSeek(baseURL, key string) *DeepSeek { return &DeepSeek{newClient(baseURL, key)} }

// Search asks for summaries only. DeepSeek bills a search that answers 200
// with results; an empty one is free.
func (d *DeepSeek) Search(ctx context.Context, query string, count int) ([]assistant.SearchHit, bool, error) {
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Summary string `json:"summary"`
		} `json:"results"`
	}
	err := d.post(ctx, "/v1/tools/search", map[string]any{"query": query, "count": count, "need_content": false}, &out, nil)
	if err != nil {
		return nil, false, err
	}
	hits := make([]assistant.SearchHit, 0, len(out.Results))
	for _, r := range out.Results {
		if r.URL == "" {
			continue
		}
		s := r.Summary
		if strings.TrimSpace(s) == "" {
			s = r.Snippet
		}
		hits = append(hits, assistant.SearchHit{Title: r.Title, URL: r.URL, Summary: s})
	}
	return hits, len(out.Results) > 0, nil
}

// Fetch reads a page as Markdown. DeepSeek bills a fetch that gives page
// content. Its Fetch reference documents 403 as the page refused by its
// security policy and 404 as no usable page content (a bad key is 401,
// no balance 402): both are about the page, not the account.
func (d *DeepSeek) Fetch(ctx context.Context, url string) (assistant.Page, bool, error) {
	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	err := d.post(ctx, "/v1/tools/fetch", map[string]any{"url": url}, &out, fetchFailure)
	if err != nil {
		return assistant.Page{}, false, err
	}
	return assistant.Page{Title: out.Title, Content: out.Content}, strings.TrimSpace(out.Content) != "", nil
}

func fetchFailure(status int) (assistant.ToolFailure, assistant.Category, bool) {
	if status == http.StatusForbidden || status == http.StatusNotFound {
		return assistant.FailureInvalid, assistant.ErrNotFound, true
	}
	return 0, "", false
}

// pageMessages are what the model is told when a call fails on its
// arguments or its page.
const (
	badRequest = "搜索服务拒绝了这次调用的参数，请检查后再试。"
	noPage     = "这个网页拿不到可读的正文（页面打不开、被站点拒绝或者类型不支持），换一个搜索结果试试。"
)

type client struct {
	baseURL, key string
	http         *http.Client
}

func newClient(baseURL, key string) client {
	return client{baseURL: strings.TrimRight(baseURL, "/"), key: key, http: &http.Client{
		// a redirect would carry the key elsewhere
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// special classifies a status the service gives its own meaning; ok is
// false for the statuses every service shares.
type special func(status int) (assistant.ToolFailure, assistant.Category, bool)

func (c client) post(ctx context.Context, path string, body, out any, statuses special) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return assistant.Transient(assistant.ErrTimeout, err)
		}
		return assistant.Transient(assistant.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return assistant.Transient(assistant.ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return c.failure(resp.StatusCode, raw, statuses)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return assistant.Transient(assistant.ErrUnavailable, fmt.Errorf("unreadable answer: %w", err))
	}
	return nil
}

// failure classifies an error status (#117): an account problem turns the
// capability off for the run, a passing one is tried again, a call the
// service refuses is the model's to fix.
func (c client) failure(status int, raw []byte, statuses special) error {
	cause := fmt.Errorf("%d: %s", status, c.scrub(bytes.TrimSpace(raw)))
	failure, reason, ok := assistant.ToolFailure(0), assistant.Category(""), false
	if statuses != nil {
		failure, reason, ok = statuses(status)
	}
	if !ok {
		switch {
		case status == http.StatusPaymentRequired:
			failure, reason = assistant.FailureAccount, assistant.ErrQuotaExhausted
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			failure, reason = assistant.FailureAccount, assistant.ErrAuth
		case status == http.StatusTooManyRequests:
			failure, reason = assistant.FailureTransient, assistant.ErrRateLimited
		case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
			failure, reason = assistant.FailureTransient, assistant.ErrTimeout
		case status >= 500:
			failure, reason = assistant.FailureTransient, assistant.ErrUnavailable
		default:
			failure, reason = assistant.FailureInvalid, assistant.ErrInvalidRequest
		}
	}
	msg := ""
	if failure == assistant.FailureInvalid {
		msg = badRequest
		if reason == assistant.ErrNotFound {
			msg = noPage
		}
	}
	return &assistant.ToolError{Failure: failure, Reason: reason, Message: msg, Cause: cause}
}

// scrub keeps the key out of what the log is told.
func (c client) scrub(b []byte) string {
	s := string(b)
	if len(s) > 500 {
		s = s[:500]
	}
	if c.key != "" {
		s = strings.ReplaceAll(s, c.key, "[key]")
	}
	return s
}

// For is the search service of a web route at the services' own
// addresses: DeepSeek searches and reads pages, Tavily only searches.
func For(route assistant.SearchRoute, key string) assistant.WebRoute {
	if route == assistant.RouteDeepSeek {
		d := NewDeepSeek(DeepSeekURL, key)
		return assistant.WebRoute{Search: d, Fetch: d}
	}
	return assistant.WebRoute{Search: NewTavily(TavilyURL, key)}
}
