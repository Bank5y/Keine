// Package adapters picks the model adapter (ADR 0008) of an endpoint by
// its interface format, for the server and keine-eval alike.
package adapters

import (
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/assistant/anthropic"
	"github.com/Bank5y/keine/internal/assistant/openai"
)

// For is the adapter of an endpoint's interface format.
func For(e assistant.Endpoint) assistant.Adapter {
	if e.Format == assistant.FormatAnthropic {
		return anthropic.New(e)
	}
	return openai.New(e)
}
