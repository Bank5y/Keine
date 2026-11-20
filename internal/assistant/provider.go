package assistant

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
}
