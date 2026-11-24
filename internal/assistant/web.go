package assistant

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"
)

// Searcher is a search service behind Keine's web_search (ADR 0008, the
// Keine tool route): Tavily, or DeepSeek Search. Billed tells whether the
// service charges for the request, which only the service's own rules
// decide; a failure is never billed. Failures are *ToolError, classified
// as the loop needs them (#117).
type Searcher interface {
	Search(ctx context.Context, query string, count int) (hits []SearchHit, billed bool, err error)
}

// Fetcher is a search service that also reads pages, behind web_fetch:
// DeepSeek Fetch. Billed as for Searcher.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (page Page, billed bool, err error)
}

// SearchHit is one result of a search, as the service gives it.
type SearchHit struct {
	Title, URL, Summary string
}

// Page is a page read by a fetch: its title and its text as Markdown.
type Page struct {
	Title, Content string
}

// WebSourceKind is how a source of a turn was reached.
type WebSourceKind string

const (
	WebSourceSearch WebSourceKind = "search"
	WebSourceFetch  WebSourceKind = "fetch"
)

// WebSource is one entry of a turn's source list (ADR 0008): what the run
// actually found or read on the web, made by the server, never by the
// model. It is the only place references come from and never goes back
// into the model's context.
type WebSource struct {
	Title string        `json:"title"`
	URL   string        `json:"url"`
	Kind  WebSourceKind `json:"kind"`
	// N is its citation number in an exploration dialogue (ADR 0013); 0
	// elsewhere, and on sources kept before citations
	N int `json:"n,omitempty"`
}

// Web tool names, which are also their activities' kinds and the names of
// the two capabilities (ADR 0008).
const (
	ToolWebSearch = "web_search"
	ToolWebFetch  = "web_fetch"
)

// Web tool limits (#118).
const (
	SearchResults  = 5
	MaxHitSummary  = 500
	MaxPageContent = 20_000
	// what DeepSeek Search takes; longer queries are a call written wrong
	maxQuery = 128
)

const (
	truncatedPage = "\n\n[正文过长，已截断，只保留了前 20000 字]"
	noHits        = "没有找到相关结果。可以换个关键词再搜，或者依据已有资料作答。"
	notSearched   = "这个 URL 不在本次运行的搜索结果里。web_fetch 只能抓取 web_search 返回过的 URL，请原样使用搜索结果里的 URL，或者先搜索。"
	emptyPage     = "这个网页拿不到可读的正文，换一个搜索结果试试。"
	longQuery     = "搜索词最多 128 个字，请缩短后再搜。"
	emptyQuery    = "搜索词不能为空。"
)

// capabilityNames are the capabilities as the model is told they are off.
var capabilityNames = map[string]string{
	ToolWebSearch: "联网搜索",
	ToolWebFetch:  "网页抓取",
}

// WebRoute is how a run reaches the web on the Keine tool route: the
// search service, the fetcher when the service reads pages, and whether
// the key is the reader's own (failures then say why). A nil route is a
// run without the web.
type WebRoute struct {
	Search Searcher
	// Fetch is nil when the service only searches (Tavily); web_fetch is
	// then not declared.
	Fetch Fetcher
	Own   bool
}

// Web is the web tools of one run and what they found: the URLs a fetch
// may read, the source list, and the requests the services bill.
type Web struct {
	route WebRoute
	mu    sync.Mutex
	// found are the URLs the run's searches gave
	found    map[string]bool
	sources  []WebSource
	seen     map[WebSource]bool
	searches int
	// cite numbers what the run finds, in a dialogue (ADR 0013)
	cite *Citations
}

func newWeb(route WebRoute) *Web {
	return &Web{route: route, found: map[string]bool{}, seen: map[WebSource]bool{}}
}

type webSearchArgs struct {
	Query string `json:"query" desc:"搜索词，最多 128 个字"`
}

type webFetchArgs struct {
	URL string `json:"url" desc:"要读的网页 URL，必须是本次运行 web_search 结果里的 URL"`
}

// Tools are web_search, and web_fetch when the route reads pages.
func (w *Web) Tools() []Tool {
	out := []Tool{NewTool(ToolWebSearch,
		fmt.Sprintf("在网上搜索，返回最多 %d 条结果（标题、URL、摘要）。本讲和课程里查不到、又需要外部资料时再用。", SearchResults),
		func(a webSearchArgs) string { return a.Query }, w.search)}
	if w.route.Fetch != nil {
		out = append(out, NewTool(ToolWebFetch,
			"读一个网页的正文（Markdown）。只能读本次运行 web_search 结果里出现过的 URL。摘要不够用时再读。",
			func(a webFetchArgs) string { return HostOf(a.URL) }, w.fetch))
	}
	return out
}

func (w *Web) search(ctx context.Context, a webSearchArgs) (string, error) {
	q := strings.TrimSpace(a.Query)
	switch {
	case q == "":
		return "", Invalid(ErrInvalidRequest, emptyQuery)
	case utf8.RuneCountInString(q) > maxQuery:
		return "", Invalid(ErrInvalidRequest, longQuery)
	}
	hits, billed, err := w.route.Search.Search(ctx, q, SearchResults)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil && billed {
		w.searches++
	}
	if err != nil {
		return "", err
	}
	if len(hits) > SearchResults {
		hits = hits[:SearchResults]
	}
	if len(hits) == 0 {
		return noHits, nil
	}
	var b strings.Builder
	for i, h := range hits {
		w.found[h.URL] = true
		s := w.add(WebSource{Title: h.Title, URL: h.URL, Kind: WebSourceSearch})
		mark := fmt.Sprintf("%d.", i+1)
		if s.N > 0 {
			mark = fmt.Sprintf("[%d]", s.N)
		}
		fmt.Fprintf(&b, "%s %s\nURL: %s\n%s\n\n", mark, h.Title, h.URL, clip(h.Summary, MaxHitSummary))
	}
	return strings.TrimSpace(b.String()), nil
}

func (w *Web) fetch(ctx context.Context, a webFetchArgs) (string, error) {
	u := strings.TrimSpace(a.URL)
	w.mu.Lock()
	ok := w.found[u]
	w.mu.Unlock()
	if !ok {
		return "", Invalid(ErrInvalidRequest, notSearched)
	}
	p, billed, err := w.route.Fetch.Fetch(ctx, u)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil && billed {
		w.searches++
	}
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(p.Content)
	if content == "" {
		return "", Invalid(ErrNotFound, emptyPage)
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = HostOf(u)
	}
	s := w.add(WebSource{Title: title, URL: u, Kind: WebSourceFetch})
	if utf8.RuneCountInString(content) > MaxPageContent {
		content = string([]rune(content)[:MaxPageContent]) + truncatedPage
	}
	if s.N > 0 {
		title = fmt.Sprintf("[%d] %s", s.N, title)
	}
	return "# " + title + "\n\n" + content, nil
}

// add records a source once, numbered in a dialogue, and returns it; w.mu
// is held.
func (w *Web) add(s WebSource) WebSource {
	if w.cite != nil {
		s.N = w.cite.number(s.URL)
	}
	if w.seen[s] {
		return s
	}
	w.seen[s] = true
	w.sources = append(w.sources, s)
	return s
}

// Sources is the run's source list, in the order found.
func (w *Web) Sources() []WebSource {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]WebSource{}, w.sources...)
}

// Searches is how many of the run's requests the services bill.
func (w *Web) Searches() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.searches
}

// clip cuts s to at most n characters.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// HostOf is the site a URL is on, as the reader sees a fetch in progress.
func HostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// ownKeyFailure tells whether a failed call of the tool ran on the
// reader's own key; false for a run without the web.
func (w *Web) ownKeyFailure(tool string) bool {
	return w != nil && w.route.Own && (tool == ToolWebSearch || tool == ToolWebFetch)
}
