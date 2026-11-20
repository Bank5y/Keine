// Package openai is the model adapter for OpenAI-compatible Chat
// Completions endpoints (ADR 0008). It speaks the wire format with
// net/http and absorbs the quirks of compatible endpoints here, so the
// assistant loop only ever sees complete tool calls and one usage figure.
package openai

import (
	"bufio"
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

type Adapter struct {
	baseURL, key string
	provider     assistant.Provider
	client       *http.Client
}

// New returns an adapter for the endpoint: BaseURL is the part before
// /chat/completions, Key may be empty for endpoints without auth, and the
// provider configuration (ADR 0008) adds that provider's rules.
func New(e assistant.Endpoint) *Adapter {
	return &Adapter{baseURL: strings.TrimRight(e.BaseURL, "/"), key: e.Key, provider: e.Provider, client: http.DefaultClient}
}

// deepseek: DeepSeek's thinking switch, its reasoning passed back within a
// run, and its cache hit and miss counts.
func (a *Adapter) deepseek() bool { return a.provider == assistant.ProviderDeepSeek }

func (a *Adapter) Stream(ctx context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	body, err := json.Marshal(a.wireRequest(req))
	if err != nil {
		return assistant.Result{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return assistant.Result{}, &assistant.Error{Category: assistant.ErrInvalidRequest, Detail: err.Error()}
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "text/event-stream")
	if a.key != "" {
		hr.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.client.Do(hr)
	if err != nil {
		return assistant.Result{}, transportError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return assistant.Result{}, statusError(resp)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		return assistant.Result{}, &assistant.Error{Category: assistant.ErrBadResponse, Detail: "not a stream: " + ct}
	}
	return readStream(ctx, resp.Body, emit, a.deepseek())
}

// --- request

type wireMessage struct {
	Role             string         `json:"role"`
	Content          *string        `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

func (a *Adapter) wireRequest(req assistant.Request) map[string]any {
	msgs := make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := m.Content
		wm := wireMessage{Role: string(m.Role), Content: &content, ToolCallID: m.ToolCallID}
		if a.deepseek() {
			// within a run, as it was (#115); a run's history never has any
			wm.ReasoningContent = m.Reasoning
		}
		if m.Role == assistant.RoleAssistant && content == "" && len(m.ToolCalls) > 0 {
			wm.Content = nil
		}
		for _, c := range m.ToolCalls {
			wc := wireToolCall{ID: c.ID, Type: "function"}
			wc.Function.Name, wc.Function.Arguments = c.Name, c.Arguments
			wm.ToolCalls = append(wm.ToolCalls, wc)
		}
		msgs = append(msgs, wm)
	}
	body := map[string]any{
		"model":          req.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		tools := make([]wireTool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i].Type = "function"
			tools[i].Function.Name, tools[i].Function.Description, tools[i].Function.Parameters = t.Name, t.Description, t.Parameters
		}
		body["tools"] = tools
		if req.ToolChoice != "" {
			body["tool_choice"] = req.ToolChoice
		}
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if a.deepseek() {
		// thinking is on unless switched off
		body["thinking"] = map[string]string{"type": map[bool]string{true: "enabled", false: "disabled"}[req.Thinking]}
	}
	return body
}

// --- response

type chunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			// Reasoning is the field name some gateways use instead.
			Reasoning string          `json:"reasoning"`
			ToolCalls []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *wireError `json:"error"`
}

type toolCallDelta struct {
	// Index is missing on some endpoints.
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireUsage struct {
	PromptTokens        *int64 `json:"prompt_tokens"`
	CompletionTokens    *int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	// DeepSeek's own counts of the prompt
	PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
}

type wireError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

func readStream(ctx context.Context, r io.Reader, emit func(assistant.Event) error, deepseek bool) (assistant.Result, error) {
	var (
		res      assistant.Result
		finish   string
		usage    *wireUsage
		asm      callAssembler
		gotChunk bool
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var data strings.Builder
	flush := func() error {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var c chunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			return &assistant.Error{Category: assistant.ErrBadResponse, Detail: "chunk: " + err.Error()}
		}
		if c.Error != nil {
			return streamError(c.Error)
		}
		gotChunk = true
		// a later usage replaces an earlier one: some endpoints repeat the
		// running total in every chunk, so adding them up would overcount
		if c.Usage != nil {
			usage = c.Usage
		}
		for _, ch := range c.Choices {
			if t := ch.Delta.ReasoningContent + ch.Delta.Reasoning; t != "" {
				if err := emit(assistant.Event{Kind: assistant.EventReasoning, Text: t}); err != nil {
					return err
				}
			}
			if ch.Delta.Content != "" {
				if err := emit(assistant.Event{Kind: assistant.EventText, Text: ch.Delta.Content}); err != nil {
					return err
				}
			}
			for _, d := range ch.Delta.ToolCalls {
				asm.add(d)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finish = *ch.FinishReason
			}
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return res, err
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteByte('\n')
		}
		// comments (":") and event/id/retry fields carry nothing for us
	}
	if err := sc.Err(); err != nil {
		return res, transportError(ctx, err)
	}
	if err := flush(); err != nil {
		return res, err
	}
	if !gotChunk {
		return res, &assistant.Error{Category: assistant.ErrBadResponse, Detail: "empty stream"}
	}
	for _, c := range asm.calls {
		if c.Arguments == "" {
			c.Arguments = "{}"
		}
		if err := emit(assistant.Event{Kind: assistant.EventToolCall, ToolCall: c}); err != nil {
			return res, err
		}
	}
	res.Finish = finishReason(finish, len(asm.calls) > 0)
	res.Usage = unify(usage)
	if deepseek && usage != nil && usage.PromptCacheHitTokens != nil && usage.PromptCacheMissTokens != nil {
		hit, miss := *usage.PromptCacheHitTokens, *usage.PromptCacheMissTokens
		res.Usage.CacheRead, res.Usage.Input = &hit, &miss
	}
	return res, nil
}

// finishReason trusts a call made over the endpoint's word, except that a
// cut-off output stays "length": its last call may be incomplete.
func finishReason(s string, called bool) assistant.FinishReason {
	if called && s != "length" {
		return assistant.FinishToolCalls
	}
	switch s {
	case "stop":
		return assistant.FinishStop
	case "length":
		return assistant.FinishLength
	case "tool_calls", "function_call":
		return assistant.FinishToolCalls
	case "content_filter":
		return assistant.FinishFiltered
	}
	return assistant.FinishOther
}

func unify(u *wireUsage) assistant.Usage {
	var out assistant.Usage
	if u == nil {
		return out
	}
	out.Output = u.CompletionTokens
	if u.CompletionTokensDetails != nil {
		out.Reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.PromptTokens != nil {
		in := *u.PromptTokens
		if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens != nil {
			cached := *u.PromptTokensDetails.CachedTokens
			out.CacheRead = &cached
			in -= cached
		}
		out.Input = &in
	}
	return out
}

// callAssembler puts streamed tool call fragments back together.
type callAssembler struct {
	calls   []*assistant.ToolCall
	byIndex map[int]*assistant.ToolCall
}

func (a *callAssembler) add(d toolCallDelta) {
	c := a.find(d)
	if c.ID == "" {
		c.ID = d.ID
	}
	// some endpoints repeat the whole name in every fragment
	if name := d.Function.Name; name != "" && name != c.Name {
		c.Name += name
	}
	c.Arguments += d.Function.Arguments
}

// find returns the call a fragment belongs to: by id when it has one (an
// unseen id starts a new call), else by index, else the latest call (some
// endpoints send neither on continuation fragments).
func (a *callAssembler) find(d toolCallDelta) *assistant.ToolCall {
	if a.byIndex == nil {
		a.byIndex = map[int]*assistant.ToolCall{}
	}
	if d.ID != "" {
		for _, c := range a.calls {
			if c.ID == d.ID {
				return c
			}
		}
		if d.Index != nil {
			if c, ok := a.byIndex[*d.Index]; ok && c.ID == "" {
				return c
			}
		}
		return a.start(d.Index)
	}
	if d.Index != nil {
		if c, ok := a.byIndex[*d.Index]; ok {
			return c
		}
		return a.start(d.Index)
	}
	if len(a.calls) == 0 {
		return a.start(nil)
	}
	return a.calls[len(a.calls)-1]
}

func (a *callAssembler) start(index *int) *assistant.ToolCall {
	c := &assistant.ToolCall{}
	a.calls = append(a.calls, c)
	if index != nil {
		a.byIndex[*index] = c
	}
	return c
}

// --- errors

func transportError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &assistant.Error{Category: assistant.ErrTimeout, Detail: err.Error()}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return &assistant.Error{Category: assistant.ErrTimeout, Detail: err.Error()}
	}
	return &assistant.Error{Category: assistant.ErrUnavailable, Detail: err.Error()}
}

func statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	detail := fmt.Sprintf("%d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	cat := assistant.ErrInvalidRequest
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		cat = assistant.ErrAuth
	case resp.StatusCode == http.StatusPaymentRequired || quotaMessage(string(raw)):
		cat = assistant.ErrQuotaExhausted
	case resp.StatusCode == http.StatusNotFound:
		cat = assistant.ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		cat = assistant.ErrRateLimited
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		cat = assistant.ErrTimeout
	case resp.StatusCode >= 500:
		cat = assistant.ErrUnavailable
	}
	return &assistant.Error{Category: cat, Detail: detail}
}

// quotaMessage spots "out of credit" answers, which OpenAI sends as 429.
func quotaMessage(body string) bool {
	return strings.Contains(body, "insufficient_quota") || strings.Contains(body, "Insufficient Balance")
}

func streamError(e *wireError) error {
	detail := fmt.Sprintf("%s %v: %s", e.Type, e.Code, e.Message)
	cat := assistant.ErrUnavailable
	switch {
	case quotaMessage(detail):
		cat = assistant.ErrQuotaExhausted
	case strings.Contains(detail, "rate_limit"):
		cat = assistant.ErrRateLimited
	case strings.Contains(detail, "invalid_request"):
		cat = assistant.ErrInvalidRequest
	}
	return &assistant.Error{Category: cat, Detail: detail}
}
