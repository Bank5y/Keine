package anthropic

import (
	"encoding/json"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/Bank5y/keine/internal/assistant"
)

// The provider's own web search and fetch (ADR 0008, the provider route).
// Anthropic runs them inside a call; the adapter turns each into a step
// the loop shows as an activity, and what they found or the answer cites
// into Keine's sources.

// maxUses caps each of the two in one call, as the Keine tool route caps
// the tool calls of one step.
const maxUses = assistant.MaxParallel

// webTools declares web_search and web_fetch. A fetch may read only what a
// search of the run found (ADR 0008): not a URL the lesson or a Keine tool
// gave.
func webTools(model string) []sdk.ToolUnionParam {
	sources := sdk.WebFetchURLSourcesParam{
		ClientToolResults: sdk.WebFetchURLSourcesClientToolResultsUnionParam{OfNone: &sdk.WebFetchURLSourceNoneParam{}},
		ServerToolResults: sdk.WebFetchURLSourcesServerToolResultsUnionParam{OfOnly: &sdk.WebFetchURLSourceOnlyParam{
			Tools: []sdk.WebFetchURLSourceToolReferenceParam{{Name: assistant.ToolWebSearch}},
		}},
		UserInput: sdk.WebFetchURLSourcesUserInputUnionParam{OfNone: &sdk.WebFetchURLSourceNoneParam{}},
	}
	if basicWeb(model) {
		return []sdk.ToolUnionParam{
			{OfWebSearchTool20250305: &sdk.WebSearchTool20250305Param{MaxUses: sdk.Int(maxUses)}},
			{OfWebFetchTool20250910: &sdk.WebFetchTool20250910Param{MaxUses: sdk.Int(maxUses), URLSources: sources}},
		}
	}
	return []sdk.ToolUnionParam{
		{OfWebSearchTool20260209: &sdk.WebSearchTool20260209Param{MaxUses: sdk.Int(maxUses)}},
		{OfWebFetchTool20260209: &sdk.WebFetchTool20260209Param{MaxUses: sdk.Int(maxUses), URLSources: sources}},
	}
}

// basicWeb tells the models that predate the search tools with dynamic
// filtering (Opus 4.6 and Sonnet 4.6 were the first); any other model gets
// those.
func basicWeb(model string) bool {
	for _, p := range []string{"claude-3", "claude-haiku-4", "claude-sonnet-4-5", "claude-sonnet-4-2", "claude-opus-4-5", "claude-opus-4-1", "claude-opus-4-2", "claude-sonnet-4-0", "claude-opus-4-0"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// webStep is what a finished block of the call tells about the provider's
// search: a step starting (a server_tool_use of web_search or web_fetch,
// whose input is complete once the block is), or ending with what it
// found; and the sources an answer's text cites.
func webStep(b sdk.ContentBlockUnion) *assistant.Event {
	switch b.Type {
	case "server_tool_use":
		kind := string(b.Name)
		if kind != assistant.ToolWebSearch && kind != assistant.ToolWebFetch {
			return nil
		}
		var in struct {
			Query string `json:"query"`
			URL   string `json:"url"`
		}
		_ = json.Unmarshal(b.Input, &in)
		target := in.Query
		if kind == assistant.ToolWebFetch {
			target = assistant.HostOf(in.URL)
		}
		return &assistant.Event{Kind: assistant.EventWeb, Web: &assistant.WebStep{ID: b.ID, Kind: kind, Target: target, Status: assistant.ActivityRunning}}
	case "web_search_tool_result":
		r := b.AsWebSearchToolResult()
		step := &assistant.WebStep{ID: r.ToolUseID, Kind: assistant.ToolWebSearch, Status: assistant.ActivityDone}
		ev := &assistant.Event{Kind: assistant.EventWeb, Web: step}
		if r.Content.ErrorCode != "" {
			step.Status, step.Reason = assistant.ActivityFailed, searchReason(r.Content.ErrorCode)
			return ev
		}
		for _, hit := range r.Content.OfWebSearchResultBlockArray {
			ev.Sources = append(ev.Sources, assistant.WebSource{Title: hit.Title, URL: hit.URL, Kind: assistant.WebSourceSearch})
		}
		return ev
	case "web_fetch_tool_result":
		r := b.AsWebFetchToolResult()
		step := &assistant.WebStep{ID: r.ToolUseID, Kind: assistant.ToolWebFetch, Status: assistant.ActivityDone}
		ev := &assistant.Event{Kind: assistant.EventWeb, Web: step}
		if r.Content.ErrorCode != "" {
			step.Status, step.Reason = assistant.ActivityFailed, fetchReason(r.Content.ErrorCode)
			return ev
		}
		title := strings.TrimSpace(r.Content.Content.Title)
		if title == "" {
			title = assistant.HostOf(r.Content.URL)
		}
		ev.Sources = []assistant.WebSource{{Title: title, URL: r.Content.URL, Kind: assistant.WebSourceFetch}}
		return ev
	case "text":
		var cited []assistant.WebSource
		for _, c := range b.Citations {
			if c.Type == "web_search_result_location" {
				cited = append(cited, assistant.WebSource{Title: c.Title, URL: c.URL, Kind: assistant.WebSourceSearch})
			}
		}
		if len(cited) > 0 {
			return &assistant.Event{Kind: assistant.EventSources, Sources: cited}
		}
	}
	return nil
}

// searchReason is a failed search's error_code as the reasons the Keine
// tool route has.
func searchReason(code sdk.WebSearchToolResultErrorCode) assistant.Category {
	switch code {
	case sdk.WebSearchToolResultErrorCodeInvalidToolInput, sdk.WebSearchToolResultErrorCodeQueryTooLong, sdk.WebSearchToolResultErrorCodeRequestTooLarge:
		return assistant.ErrInvalidRequest
	case sdk.WebSearchToolResultErrorCodeTooManyRequests:
		return assistant.ErrRateLimited
	}
	// unavailable, max_uses_exceeded (the call's own cap, not the
	// account's), and codes not known yet
	return assistant.ErrUnavailable
}

// fetchReason is a failed fetch's error_code as the reasons the Keine tool
// route has.
func fetchReason(code sdk.WebFetchToolResultErrorCode) assistant.Category {
	switch code {
	case sdk.WebFetchToolResultErrorCodeInvalidToolInput, sdk.WebFetchToolResultErrorCodeURLTooLong,
		sdk.WebFetchToolResultErrorCodeURLNotAllowed, sdk.WebFetchToolResultErrorCodeURLNotInPriorContext:
		return assistant.ErrInvalidRequest
	case sdk.WebFetchToolResultErrorCodeURLNotAccessible:
		return assistant.ErrNotFound
	case sdk.WebFetchToolResultErrorCodeUnsupportedContentType, sdk.WebFetchToolResultErrorCodeContentTooLarge:
		return assistant.ReasonUnsupportedContent
	case sdk.WebFetchToolResultErrorCodeTooManyRequests:
		return assistant.ErrRateLimited
	}
	return assistant.ErrUnavailable
}
