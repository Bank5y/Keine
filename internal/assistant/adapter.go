// Package assistant is the exploration assistant (ADR 0008): the site's
// models, the mapping from turn kinds to them, and the model adapter
// boundary the assistant loop talks to.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Adapter makes one streaming model call. It is the only replaceable unit
// of the assistant (ADR 0008): it hides the provider's wire format and
// quirks, and hands back text and reasoning deltas as they arrive, complete
// tool calls, unified usage and the finish reason.
type Adapter interface {
	// Stream sends req and calls emit for each event, in order. An error
	// from emit stops the call and is returned as is. Provider failures are
	// returned as *Error.
	Stream(ctx context.Context, req Request, emit func(Event) error) (Result, error)
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Request is one call in Keine's own terms; adapters translate it.
type Request struct {
	Model    string
	Messages []Message
	Tools    []ToolSpec
	// ToolChoice is "" (provider default), "auto", "none" or "required"
	// (some tool must be called; never with Thinking).
	ToolChoice string
	// MaxTokens caps the output; 0 leaves it to the provider.
	MaxTokens int
	// Thinking switches the model's thinking on or off where the provider
	// configuration has a switch; elsewhere it changes nothing.
	Thinking bool
	// ProviderWeb declares the provider's own web_search and web_fetch
	// besides Tools (ADR 0008, the provider route), for adapters whose
	// format has them (FormatSpec.ProviderWeb). ToolChoice "none" keeps
	// them from being used too.
	ProviderWeb bool
}

type Message struct {
	Role    Role
	Content string
	// Reasoning is passed back within one run only (assistant messages).
	Reasoning string
	// ToolCalls are the calls an assistant message made.
	ToolCalls []ToolCall
	// ToolCallID names the call a tool message answers.
	ToolCallID string
	// Native is an assistant message in its adapter's own form, from the
	// Result of the call that wrote it: what the provider wants back as it
	// was (Anthropic's thinking blocks with their signatures, its search
	// results). Within one run only: it is opaque to the loop and never
	// stored (ADR 0008).
	Native any
}

// ToolSpec is a tool declaration: Parameters is its JSON Schema.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is a complete call: Arguments is the JSON text the model wrote,
// which may be malformed (the loop answers that with a fix-it result).
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type EventKind string

const (
	EventText      EventKind = "text"
	EventReasoning EventKind = "reasoning"
	// EventToolCall carries one complete tool call.
	EventToolCall EventKind = "tool_call"
	// EventWeb is a step of the provider's own search (the provider route,
	// ADR 0008): a search or a fetch as it starts, and as it ends with what
	// it found.
	EventWeb EventKind = "web"
	// EventSources carries sources the answer cites, besides those its
	// searches found.
	EventSources EventKind = "sources"
)

type Event struct {
	Kind     EventKind
	Text     string
	ToolCall *ToolCall
	Web      *WebStep
	// Sources are what a done step or a citation adds to the source list.
	Sources []WebSource
}

// WebStep is a search or a fetch the provider made, as the adapter
// normalizes it: an ID the start and the end share, the capability
// (ToolWebSearch or ToolWebFetch), what it was about, and how it went.
// The provider's errors map to the reasons the Keine tool route has; they
// are neither tried again nor turn the capability off (ADR 0008).
type WebStep struct {
	ID     string
	Kind   string
	Target string
	Status ActivityStatus
	Reason Category
}

type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishLength    FinishReason = "length"
	FinishToolCalls FinishReason = "tool_calls"
	FinishFiltered  FinishReason = "content_filter"
	FinishOther     FinishReason = "other"
)

// Usage is the unified usage of one call. A nil field is one the provider
// did not report. Input excludes the cache hits counted in CacheRead, so
// the four token fields never overlap; Reasoning is a part of Output.
type Usage struct {
	Input      *int64 `json:"input"`
	Output     *int64 `json:"output"`
	CacheRead  *int64 `json:"cacheRead"`
	CacheWrite *int64 `json:"cacheWrite"`
	Reasoning  *int64 `json:"reasoning"`
	// Searches are the provider's own searches the call is billed for
	// (the provider route); nil when it made none it bills.
	Searches *int64 `json:"searches,omitempty"`
}

// Add adds what another call used: a field either reported counts, and
// stays nil only when neither did.
func (u *Usage) Add(o Usage) {
	for _, f := range []struct {
		to   **int64
		from *int64
	}{
		{&u.Input, o.Input}, {&u.Output, o.Output}, {&u.CacheRead, o.CacheRead},
		{&u.CacheWrite, o.CacheWrite}, {&u.Reasoning, o.Reasoning}, {&u.Searches, o.Searches},
	} {
		if f.from == nil {
			continue
		}
		n := *f.from
		if *f.to != nil {
			n += **f.to
		}
		*f.to = &n
	}
}

// reported tells whether the provider said what the call used.
func (u Usage) reported() bool { return u.Input != nil || u.Output != nil }

type Result struct {
	Usage  Usage
	Finish FinishReason
	// Native is the call's message in the adapter's own form, for the
	// Native of the assistant message the loop passes back; nil when the
	// adapter rebuilds messages from their Keine form.
	Native any
}

// Category is what kind of failure a call met, the only detail of a
// provider error that leaves the server.
type Category string

const (
	ErrTimeout        Category = "timeout"
	ErrRateLimited    Category = "rate_limited"
	ErrQuotaExhausted Category = "quota_exhausted"
	ErrAuth           Category = "auth"
	ErrUnavailable    Category = "unavailable"
	ErrNotFound       Category = "not_found"
	ErrInvalidRequest Category = "invalid_request"
	// ErrBadResponse: the endpoint answered, but not with a stream Keine
	// can read.
	ErrBadResponse Category = "bad_response"
	// ErrBlocked: the address is one Keine may not connect to (a reader's
	// own model at an inside address, ADR 0010).
	ErrBlocked Category = "blocked_address"
)

// Error is a provider failure. Detail is for the server log only.
type Error struct {
	Category Category
	Detail   string
}

func (e *Error) Error() string { return fmt.Sprintf("model call: %s: %s", e.Category, e.Detail) }

// CategoryOf returns the category of a failed call, or "" if err is not a
// provider failure.
func CategoryOf(err error) Category {
	var e *Error
	if errors.As(err, &e) {
		return e.Category
	}
	return ""
}
