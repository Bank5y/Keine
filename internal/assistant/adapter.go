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
	// ToolChoice is "" (provider default), "auto" or "none".
	ToolChoice string
	// MaxTokens caps the output; 0 leaves it to the provider.
	MaxTokens int
	// Thinking switches the model's thinking on or off where the provider
	// configuration has a switch; elsewhere it changes nothing.
	Thinking bool
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
)

type Event struct {
	Kind     EventKind
	Text     string
	ToolCall *ToolCall
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
}

type Result struct {
	Usage  Usage
	Finish FinishReason
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
