package assistant

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SearchRoute is how a site model's runs reach the web (ADR 0008): on the
// Keine tool route, through which search service; or not at all.
type SearchRoute string

const (
	// RouteAuto resolves from the provider and what is configured.
	RouteAuto SearchRoute = ""
	// RouteTavily searches with the site's Tavily key; it reads no pages.
	RouteTavily SearchRoute = "tavily"
	// RouteDeepSeek searches and reads pages with DeepSeek Search / Fetch,
	// on the DeepSeek model's own key.
	RouteDeepSeek SearchRoute = "deepseek"
	// RouteOff is the deployer's choice of no web.
	RouteOff SearchRoute = "off"
)

func validRoute(r SearchRoute) bool {
	switch r {
	case RouteAuto, RouteTavily, RouteDeepSeek, RouteOff:
		return true
	}
	return false
}

// Resolve is the route a model's runs take: the chosen one when what it
// needs is there, or for RouteAuto the provider's own search service, else
// Tavily when the site has a key. ok is false when nothing resolves, which
// leaves the assistant unavailable.
func Resolve(route SearchRoute, provider Provider, tavilyKey bool) (SearchRoute, bool) {
	switch route {
	case RouteOff:
		return RouteOff, true
	case RouteDeepSeek:
		return RouteDeepSeek, provider == ProviderDeepSeek
	case RouteTavily:
		return RouteTavily, tavilyKey
	}
	if provider == ProviderDeepSeek {
		return RouteDeepSeek, true
	}
	if tavilyKey {
		return RouteTavily, true
	}
	return RouteAuto, false
}

// SearchFor builds the search service of a resolved route on key.
type SearchFor func(route SearchRoute, key string) WebRoute

// a test search when the admin saves a Tavily key
const testQuery = "Keine"

const tavilyPurpose = "site_tavily"

// ErrTavilyKeyEmpty: the admin saved an empty Tavily key.
var ErrTavilyKeyEmpty = errors.New("empty tavily key")

// TavilyKey is the site's Tavily key as the admin page shows it.
type TavilyKey struct {
	Set      bool   `json:"set"`
	KeyLast4 string `json:"keyLast4"`
}

// Tavily returns whether the site has a Tavily key, and its last four.
func (s *Models) Tavily(ctx context.Context) (TavilyKey, error) {
	var k TavilyKey
	err := s.db.QueryRow(ctx, "SELECT key_last4 FROM site_tavily").Scan(&k.KeyLast4)
	if errors.Is(err, pgx.ErrNoRows) {
		return TavilyKey{}, nil
	}
	k.Set = err == nil
	return k, err
}

// tavilyKey opens the stored key; "" when there is none.
func (s *Models) tavilyKey(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (string, error) {
	var sealed []byte
	err := q.QueryRow(ctx, "SELECT api_key FROM site_tavily").Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if s.box == nil {
		return "", ErrMasterKeyMissing
	}
	key, err := s.box.Open(sealed, tavilyPurpose)
	if err != nil {
		return "", ErrKeyUnreadable
	}
	return key, nil
}

// SetTavily tests the key with one real search and stores it.
func (s *Models) SetTavily(ctx context.Context, key string) (TavilyKey, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return TavilyKey{}, ErrTavilyKeyEmpty
	}
	if s.box == nil {
		return TavilyKey{}, ErrMasterKeyMissing
	}
	testCtx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	if _, _, err := s.searchFor(RouteTavily, key).Search.Search(testCtx, testQuery, 1); err != nil {
		return TavilyKey{}, searchTestFailure(err, key)
	}
	last4 := lastFour(key)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO site_tavily (id, api_key, key_last4) VALUES (true, $1, $2)
		ON CONFLICT (id) DO UPDATE SET api_key = EXCLUDED.api_key, key_last4 = EXCLUDED.key_last4, updated_at = now()`,
		s.box.Seal(key, tavilyPurpose), last4); err != nil {
		return TavilyKey{}, err
	}
	return TavilyKey{Set: true, KeyLast4: last4}, nil
}

// ClearTavily removes the site's Tavily key; models resolving to Tavily
// leave the assistant unavailable until one is set again.
func (s *Models) ClearTavily(ctx context.Context) error {
	_, err := s.db.Exec(ctx, "DELETE FROM site_tavily")
	return err
}

// searchTestFailure is a failed test search as a failed connection test.
func searchTestFailure(err error, key string) error {
	cause := errors.New(strings.ReplaceAll(err.Error(), key, "[key]"))
	var te *ToolError
	if errors.As(err, &te) {
		return &TestFailedError{Category: te.Reason, Cause: cause, Tavily: true}
	}
	return &TestFailedError{Category: ErrUnavailable, Cause: cause, Tavily: true}
}

// routeOf is the web route of a resolved search route, on the model's key
// for DeepSeek and the site's Tavily key for Tavily; nil when off.
func (s *Models) routeOf(route SearchRoute, modelKey, tavilyKey string) *WebRoute {
	switch route {
	case RouteDeepSeek:
		w := s.searchFor(RouteDeepSeek, modelKey)
		return &w
	case RouteTavily:
		w := s.searchFor(RouteTavily, tavilyKey)
		return &w
	}
	return nil
}
