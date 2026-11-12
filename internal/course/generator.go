package course

import (
	"context"
	_ "embed"
)

// Generator helps a user produce a course manifest for a package that does
// not have one yet. The first version only hands out a prompt template the
// user gives to an AI assistant; an agent that drafts the manifest itself
// will be another Generator, and its draft still goes through the one import
// path after the user confirms it (ADR-0002).
type Generator interface {
	Name() string
	Help(ctx context.Context) (Artifact, error)
}

// Artifact is what a Generator hands the user.
type Artifact struct {
	Filename    string
	ContentType string
	Body        []byte
}

//go:embed prompt.md
var promptTemplate []byte

// PromptTemplate is the Generator that hands out the manifest prompt.
type PromptTemplate struct{}

func (PromptTemplate) Name() string { return "prompt-template" }

func (PromptTemplate) Help(context.Context) (Artifact, error) {
	return Artifact{Filename: "keine-course-json-prompt.md", ContentType: "text/markdown; charset=utf-8", Body: promptTemplate}, nil
}
