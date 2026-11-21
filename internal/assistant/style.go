package assistant

import (
	_ "embed"
	"strings"
)

// Style is an answer style (ADR 0011): tone, length and wording, shared by
// every turn's system prompt; a turn records its ID and version.
type Style struct {
	ID      string
	Version int
	Text    string
}

//go:embed prompts/styles/computing-1.md
var computingStyle string

// DefaultStyle is the style for computing content, the one every turn
// answers in for now.
var DefaultStyle = Style{ID: "computing", Version: 1, Text: strings.TrimSpace(computingStyle)}

// system puts a skill's own prompt and the style together.
func (s Style) system(skill string) string {
	return strings.TrimSpace(skill) + "\n\n" + s.Text
}
