package server_test

import (
	"net/http"
	"testing"
)

// me is what /api/me says about the signed-in reader.
func me(t *testing.T, c *client) map[string]any {
	t.Helper()
	return c.mustOK(c.do("GET", "/api/me", nil)).json()
}

func TestTheNoticeShowsUntilTheReaderFirstAnalyzesOnAnyDevice(t *testing.T) {
	a := aiApp(t, newScriptedModel())
	bob := a.user("bob")
	if got := me(t, bob); got["aiNoticeRead"] != false {
		t.Fatalf("a new reader has read the notice: %v", got)
	}

	expectStatus(t, bob.do("POST", "/api/me/ai-notice", nil), http.StatusNoContent)
	if got := me(t, bob); got["aiNoticeRead"] != true {
		t.Fatalf("after reading, aiNoticeRead = %v", got["aiNoticeRead"])
	}
	// another device is another session
	other := a.as("bob", "bob-password")
	if got := me(t, other); got["aiNoticeRead"] != true {
		t.Fatalf("on another device, aiNoticeRead = %v", got["aiNoticeRead"])
	}
	// it is the reader's own: another reader still sees it
	if got := me(t, a.user("alice")); got["aiNoticeRead"] != false {
		t.Fatalf("another reader's aiNoticeRead = %v", got["aiNoticeRead"])
	}
}

func TestTheNoticeNamesTheAnalysisModelAndItsSearchService(t *testing.T) {
	f := newFakeSearch()
	a := newApp(t, withModel(newScriptedModel()), withMasterKey(masterKey), withSearch(f))
	bob := a.user("bob")
	if got := me(t, bob); got["aiProvider"] != "" || got["aiSearch"] != "" {
		t.Fatalf("without a model, provider %q search %q", got["aiProvider"], got["aiSearch"])
	}

	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/tavily", map[string]any{"key": "tvly-site-key-1234"}))
	mapModel(t, a, map[string]any{"displayName": "DeepSeek Flash"}, false)
	if got := me(t, bob); got["aiProvider"] != "DeepSeek Flash" || got["aiSearch"] != "Tavily" {
		t.Fatalf("provider %q search %q", got["aiProvider"], got["aiSearch"])
	}

	mapModel(t, a, map[string]any{"displayName": "Claude", "format": "anthropic", "baseUrl": "https://api.anthropic.example"}, false)
	if got := me(t, bob); got["aiProvider"] != "Claude" || got["aiSearch"] != "Claude" {
		t.Fatalf("on the provider route, provider %q search %q", got["aiProvider"], got["aiSearch"])
	}

	mapModel(t, a, map[string]any{"displayName": "Offline", "searchRoute": "off"}, false)
	if got := me(t, bob); got["aiProvider"] != "Offline" || got["aiSearch"] != "" {
		t.Fatalf("with no web, provider %q search %q", got["aiProvider"], got["aiSearch"])
	}
}

func TestReadingTheNoticeNeedsASignedInReader(t *testing.T) {
	a := newApp(t)
	expectError(t, a.client().do("POST", "/api/me/ai-notice", nil), http.StatusUnauthorized, "unauthenticated")
}
