// Package anthropic is the model adapter for the Anthropic Messages API
// (ADR 0008), on the official SDK. It marks the prompt for caching, hands
// back complete tool calls and one usage figure, and keeps whatever the
// API wants back as it was (thinking blocks with their signatures) in the
// run's memory only.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/netguard"
)

// defaultMaxTokens caps a call that leaves the output to the provider: the
// API needs a cap on every call.
const defaultMaxTokens = 4096

type Adapter struct {
	client sdk.Client
}

// New returns an adapter for the endpoint: BaseURL is the part before
// /v1/messages (the format's default fills in an empty one when the model
// is saved).
func New(e assistant.Endpoint) *Adapter {
	base := strings.TrimRight(e.BaseURL, "/")
	opts := []option.RequestOption{
		option.WithBaseURL(base + "/"),
		option.WithAPIKey(e.Key),
		// the loop decides what is tried again (ADR 0008)
		option.WithMaxRetries(0),
	}
	if e.Client != nil {
		opts = append(opts, option.WithHTTPClient(e.Client))
	}
	return &Adapter{client: sdk.NewClient(opts...)}
}

// maxResumes bounds how often one call resumes a paused turn.
const maxResumes = 5

// Stream makes one call. A turn the provider paused in the middle of its
// own search (pause_turn) is resumed within the call, by sending it back
// as it came, so the loop sees one message: its text, its tool calls, the
// usage of every request, and the whole turn to pass back (ADR 0008).
func (a *Adapter) Stream(ctx context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	// current models refuse a prefill: a cut-off reply is asked to go on
	req, emit, held := assistant.AskToContinue(req, emit)
	params := wireRequest(req)
	asked := params.Messages
	var (
		content []sdk.ContentBlockUnion
		turn    native
		usage   assistant.Usage
		stop    sdk.StopReason
	)
	targets := map[string]string{}
	for resumed := 0; ; resumed++ {
		msg, err := a.once(ctx, params, targets, emit)
		if err != nil {
			return assistant.Result{}, err
		}
		content = append(content, msg.Content...)
		turn = append(turn, msg.ToParam().OfAssistant.Content...)
		usage = sumUsage(usage, unify(msg.Usage))
		stop = msg.StopReason
		if stop != sdk.StopReasonPauseTurn || resumed == maxResumes {
			break
		}
		// no message after it: the API resumes a turn that ends in its own
		// tool use
		params.Messages = append(asked[:len(asked):len(asked)], sdk.NewAssistantMessage(turn...))
	}
	if err := held(); err != nil {
		return assistant.Result{}, err
	}
	called, err := emitCalls(content, emit)
	if err != nil {
		return assistant.Result{}, err
	}
	return assistant.Result{Usage: usage, Finish: finishReason(stop, called), Native: turn}, nil
}

// once makes one request and returns its whole message, passing on what
// streams as it arrives.
func (a *Adapter) once(ctx context.Context, params sdk.MessageNewParams, targets map[string]string, emit func(assistant.Event) error) (sdk.Message, error) {
	stream := a.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var msg sdk.Message
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return msg, &assistant.Error{Category: assistant.ErrBadResponse, Detail: err.Error()}
		}
		if err := handle(ev, msg, targets, emit); err != nil {
			return msg, err
		}
	}
	if err := stream.Err(); err != nil {
		return msg, callError(ctx, err)
	}
	if msg.ID == "" {
		return msg, &assistant.Error{Category: assistant.ErrBadResponse, Detail: "empty stream"}
	}
	return msg, nil
}

// handle passes on what streams as it arrives: text and reasoning, and
// each step of the provider's search once its block is complete (msg is
// the message so far; targets keeps what each step is about, which its
// end repeats).
func handle(ev sdk.MessageStreamEventUnion, msg sdk.Message, targets map[string]string, emit func(assistant.Event) error) error {
	switch ev.Type {
	case "content_block_stop":
		if i := int(ev.Index); i < len(msg.Content) {
			if e := webStep(msg.Content[i]); e != nil {
				if w := e.Web; w != nil {
					if w.Status == assistant.ActivityRunning {
						targets[w.ID] = w.Target
					} else {
						w.Target = targets[w.ID]
					}
				}
				return emit(*e)
			}
		}
	case "content_block_start":
		if ev.ContentBlock.Type == "thinking" || ev.ContentBlock.Type == "redacted_thinking" {
			// the reasoning may come back empty (display omitted); the
			// reader still sees that the model thinks
			return emit(assistant.Event{Kind: assistant.EventReasoning})
		}
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text != "" {
				return emit(assistant.Event{Kind: assistant.EventText, Text: ev.Delta.Text})
			}
		case "thinking_delta":
			if ev.Delta.Thinking != "" {
				return emit(assistant.Event{Kind: assistant.EventReasoning, Text: ev.Delta.Thinking})
			}
		}
	}
	return nil
}

// emitCalls hands back the complete tool calls of a whole turn, and
// tells whether there were any.
func emitCalls(content []sdk.ContentBlockUnion, emit func(assistant.Event) error) (bool, error) {
	called := false
	for _, b := range content {
		if b.Type != "tool_use" {
			continue
		}
		called = true
		args := string(b.Input)
		if args == "" {
			args = "{}"
		}
		if err := emit(assistant.Event{Kind: assistant.EventToolCall, ToolCall: &assistant.ToolCall{ID: b.ID, Name: b.Name, Arguments: args}}); err != nil {
			return called, err
		}
	}
	return called, nil
}

// sumUsage sums the usage of the requests of one call.
func sumUsage(a, b assistant.Usage) assistant.Usage {
	sum := func(x, y *int64) *int64 {
		if x == nil {
			return y
		}
		if y == nil {
			return x
		}
		v := *x + *y
		return &v
	}
	return assistant.Usage{
		Input: sum(a.Input, b.Input), Output: sum(a.Output, b.Output),
		CacheRead: sum(a.CacheRead, b.CacheRead), CacheWrite: sum(a.CacheWrite, b.CacheWrite),
		Reasoning: sum(a.Reasoning, b.Reasoning), Searches: sum(a.Searches, b.Searches),
	}
}

// native is a message's content blocks as the API wants them back within
// the run: thinking with its signature, search results as they came.
type native []sdk.ContentBlockParamUnion

func unify(u sdk.Usage) assistant.Usage {
	in, out, read, write := u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens
	usage := assistant.Usage{Input: &in, Output: &out, CacheRead: &read, CacheWrite: &write}
	if n := u.ServerToolUse.WebSearchRequests; n > 0 {
		// fetches are billed by their tokens only
		usage.Searches = &n
	}
	if u.OutputTokensDetails.JSON.ThinkingTokens.Valid() {
		r := u.OutputTokensDetails.ThinkingTokens
		usage.Reasoning = &r
	}
	return usage
}

func finishReason(s sdk.StopReason, called bool) assistant.FinishReason {
	switch s {
	// a turn still paused after the resumes did not finish: like one cut
	// off, its answer is incomplete
	case sdk.StopReasonMaxTokens, sdk.StopReasonModelContextWindowExceeded, sdk.StopReasonPauseTurn:
		return assistant.FinishLength
	case sdk.StopReasonToolUse:
		return assistant.FinishToolCalls
	case sdk.StopReasonRefusal:
		return assistant.FinishFiltered
	case sdk.StopReasonEndTurn, sdk.StopReasonStopSequence:
		if called {
			return assistant.FinishToolCalls
		}
		return assistant.FinishStop
	}
	return assistant.FinishOther
}

// --- request

func wireRequest(req assistant.Request) sdk.MessageNewParams {
	p := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		// the cache marks (spec #123): the system prompt and the tools are
		// the same for every run, and each call of a run extends the last
		CacheControl: sdk.NewCacheControlEphemeralParam(),
	}
	if req.Thinking {
		p.Thinking = sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}}
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = defaultMaxTokens
	}
	var system []string
	for _, m := range req.Messages {
		if m.Role == assistant.RoleSystem {
			system = append(system, m.Content)
		}
	}
	if len(system) > 0 {
		p.System = []sdk.TextBlockParam{{Text: strings.Join(system, "\n\n"), CacheControl: sdk.NewCacheControlEphemeralParam()}}
	}
	p.Messages = wireMessages(req.Messages)
	for _, t := range req.Tools {
		p.Tools = append(p.Tools, sdk.ToolUnionParam{OfTool: &sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: inputSchema(t.Parameters),
		}})
	}
	if req.ProviderWeb {
		p.Tools = append(p.Tools, webTools(req.Model)...)
	}
	if len(p.Tools) > 0 {
		switch req.ToolChoice {
		case "auto":
			p.ToolChoice = sdk.ToolChoiceUnionParam{OfAuto: &sdk.ToolChoiceAutoParam{}}
		case "none":
			p.ToolChoice = sdk.ToolChoiceUnionParam{OfNone: &sdk.ToolChoiceNoneParam{}}
		case "required":
			p.ToolChoice = sdk.ToolChoiceUnionParam{OfAny: &sdk.ToolChoiceAnyParam{}}
		}
	}
	return p
}

// inputSchema is a tool's JSON Schema as the SDK carries it: properties
// and required in their fields, anything else as it was.
func inputSchema(raw json.RawMessage) sdk.ToolInputSchemaParam {
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	s := sdk.ToolInputSchemaParam{Properties: fields["properties"], ExtraFields: map[string]any{}}
	if req, ok := fields["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	for k, v := range fields {
		if k != "properties" && k != "required" && k != "type" {
			s.ExtraFields[k] = v
		}
	}
	return s
}

// wireMessages turns a run's messages into alternating turns: a tool
// message is a tool_result block of the user turn after the call, and a
// user message right after tool results joins that turn.
func wireMessages(msgs []assistant.Message) []sdk.MessageParam {
	var out []sdk.MessageParam
	add := func(role assistant.Role, blocks ...sdk.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			switch {
			case role == assistant.RoleUser && last.OfUser != nil:
				last.OfUser.Content = append(last.OfUser.Content, blocks...)
				return
			case role == assistant.RoleAssistant && last.OfAssistant != nil:
				last.OfAssistant.Content = append(last.OfAssistant.Content, blocks...)
				return
			}
		}
		if role == assistant.RoleUser {
			out = append(out, sdk.NewUserMessage(blocks...))
		} else {
			out = append(out, sdk.NewAssistantMessage(blocks...))
		}
	}
	for _, m := range msgs {
		switch m.Role {
		case assistant.RoleUser:
			add(assistant.RoleUser, textBlocks(m.Content)...)
		case assistant.RoleTool:
			add(assistant.RoleUser, sdk.NewToolResultBlock(m.ToolCallID, m.Content, false))
		case assistant.RoleAssistant:
			add(assistant.RoleAssistant, assistantBlocks(m)...)
		}
	}
	return out
}

func textBlocks(s string) []sdk.ContentBlockParamUnion {
	if strings.TrimSpace(s) == "" {
		// the API takes no empty text block
		return nil
	}
	return []sdk.ContentBlockParamUnion{sdk.NewTextBlock(s)}
}

// assistantBlocks is an assistant message as the API wants it back: as
// it came, when this adapter wrote it within the run.
func assistantBlocks(m assistant.Message) []sdk.ContentBlockParamUnion {
	if n, ok := m.Native.(native); ok {
		return n
	}
	blocks := textBlocks(m.Content)
	for _, c := range m.ToolCalls {
		blocks = append(blocks, sdk.NewToolUseBlock(c.ID, rawArgs(c.Arguments), c.Name))
	}
	return blocks
}

// --- errors

func callError(ctx context.Context, err error) error {
	if netguard.Blocked(err) {
		return &assistant.Error{Category: assistant.ErrBlocked, Detail: err.Error()}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &assistant.Error{Category: assistant.ErrTimeout, Detail: err.Error()}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		return &assistant.Error{Category: apiCategory(apiErr.StatusCode, string(apiErr.Type())), Detail: err.Error()}
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return &assistant.Error{Category: assistant.ErrTimeout, Detail: err.Error()}
	}
	return &assistant.Error{Category: assistant.ErrUnavailable, Detail: err.Error()}
}

// apiCategory is what an API error means, by its type first: an error in
// the middle of a stream comes with the stream's 200.
func apiCategory(status int, kind string) assistant.Category {
	switch kind {
	case "authentication_error", "permission_error":
		return assistant.ErrAuth
	case "billing_error":
		return assistant.ErrQuotaExhausted
	case "not_found_error":
		return assistant.ErrNotFound
	case "rate_limit_error":
		return assistant.ErrRateLimited
	case "overloaded_error", "api_error":
		return assistant.ErrUnavailable
	case "timeout_error":
		return assistant.ErrTimeout
	case "invalid_request_error", "request_too_large":
		return assistant.ErrInvalidRequest
	}
	switch {
	case status == 401 || status == 403:
		return assistant.ErrAuth
	case status == 402:
		return assistant.ErrQuotaExhausted
	case status == 404:
		return assistant.ErrNotFound
	case status == 429:
		return assistant.ErrRateLimited
	case status == 408 || status == 504:
		return assistant.ErrTimeout
	case status >= 400 && status < 500:
		return assistant.ErrInvalidRequest
	}
	return assistant.ErrUnavailable
}

// rawArgs is a call's arguments as the input of a tool_use block: the JSON
// the model wrote, or an empty object when it was cut off.
func rawArgs(args string) json.RawMessage {
	if json.Valid([]byte(args)) && strings.HasPrefix(strings.TrimSpace(args), "{") {
		return json.RawMessage(args)
	}
	return json.RawMessage(`{}`)
}
