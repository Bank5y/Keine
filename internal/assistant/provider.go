package assistant

import "net/http"

// Provider is a provider configuration (ADR 0008): not an adapter of its
// own, but defaults and rules the generic adapter applies for one
// provider: its default address and display name, the thinking switch,
// passing reasoning back within a run, and how its usage fields read.
type Provider string

const (
	// ProviderNone is a plain endpoint of the interface format.
	ProviderNone     Provider = ""
	ProviderDeepSeek Provider = "deepseek"
)

// ProviderPreset is what choosing a provider fills in for the admin.
type ProviderPreset struct {
	ID          Provider `json:"id"`
	Name        string   `json:"name"`
	Format      string   `json:"format"`
	BaseURL     string   `json:"baseUrl"`
	DisplayName string   `json:"displayName"`
}

// ProviderPresets are the provider configurations there are.
var ProviderPresets = []ProviderPreset{
	{ID: ProviderDeepSeek, Name: "DeepSeek", Format: FormatOpenAI, BaseURL: "https://api.deepseek.com", DisplayName: "DeepSeek"},
}

// preset returns the preset of a provider; ok is false for none or an
// unknown one.
func preset(p Provider) (ProviderPreset, bool) {
	for _, pp := range ProviderPresets {
		if pp.ID == p {
			return pp, true
		}
	}
	return ProviderPreset{}, false
}

// Endpoint is how to reach a model: what an adapter is built for.
type Endpoint struct {
	Format   string
	Provider Provider
	BaseURL  string
	Key      string
	// Client is what the adapter calls through; nil is the default
	// client. A reader's own model gets the SSRF guard's (ADR 0010).
	Client *http.Client
	// Conservative takes the capabilities an endpoint of unknown make is
	// sure to have (ADR 0010): an OpenAI-compatible one is asked for no
	// parallel tool calls.
	Conservative bool
}

// FormatSpec is an interface format as its adapter declares it (ADR
// 0008): the default address, and whether the provider runs web_search and
// web_fetch itself.
type FormatSpec struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// ProviderWeb is what the provider's own search is like; the provider
	// route needs both.
	ProviderWeb ProviderWeb `json:"-"`
}

// ProviderWeb is a provider's own search as the provider route needs it:
// the model decides whether to search (tool_choice auto, never forced),
// and the search is declared alongside Keine's tools.
type ProviderWeb struct {
	ModelDecides bool
	WithTools    bool
}

// Usable tells whether the provider route can take it.
func (w ProviderWeb) Usable() bool { return w.ModelDecides && w.WithTools }

// Formats are the interface formats there are. The Messages API's search
// and fetch qualify; OpenAI-compatible endpoints have none that do (#99).
var Formats = []FormatSpec{
	{ID: FormatOpenAI, Name: "OpenAI 兼容"},
	{ID: FormatAnthropic, Name: "Anthropic Messages", BaseURL: "https://api.anthropic.com", ProviderWeb: ProviderWeb{ModelDecides: true, WithTools: true}},
}

// formatOf returns a format; ok is false for an unknown one.
func formatOf(id string) (FormatSpec, bool) {
	for _, f := range Formats {
		if f.ID == id {
			return f, true
		}
	}
	return FormatSpec{}, false
}

// DefaultBaseURL is the address a model of the format and provider is at
// when none is given: the provider's, else the format's ("" for a plain
// OpenAI-compatible endpoint, which has none).
func DefaultBaseURL(format string, provider Provider) string {
	if p, ok := preset(provider); ok {
		return p.BaseURL
	}
	f, _ := formatOf(format)
	return f.BaseURL
}

// providerWeb tells whether models of the format can take the provider
// route.
func providerWeb(format string) bool {
	f, _ := formatOf(format)
	return f.ProviderWeb.Usable()
}
