// Package eval is keine-eval (#140): it runs the analysis of real courses
// on the real loop, context assembly, prompts, checks and course reading,
// and judges what comes out. It only reads: nothing is pinned, no usage is
// recorded, no instance is signed in to.
package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
)

// Case is one selection to analyze: a lesson of a course version, the
// selected text, and words the analysis must or must not have; with
// Turns, the dialogue after it too.
type Case struct {
	ID string `yaml:"id" json:"id"`
	// Category is the kind of course (the file the case is in).
	Category string `yaml:"-" json:"category"`
	// Kind is what the selection is: a term, a sentence, a term easy to
	// misread, one in code or a table, one in a long lesson.
	Kind   string `yaml:"kind" json:"kind"`
	Course int64  `yaml:"course" json:"course"`
	// Version is the course version (its files folder) the case was
	// written against; a course updated since is reported, not analyzed.
	Version string `yaml:"version" json:"version"`
	// Lesson is the lesson's key.
	Lesson string `yaml:"lesson" json:"lesson"`
	Quote  string `yaml:"quote" json:"quote"`
	// Prefix and Suffix tell the quote apart when it is in the lesson more
	// than once.
	Prefix string `yaml:"prefix" json:"prefix,omitempty"`
	Suffix string `yaml:"suffix" json:"suffix,omitempty"`
	// Length is the reader's choice for a long lesson.
	Length         assistant.Length `yaml:"length" json:"length,omitempty"`
	MustInclude    []string         `yaml:"must_include" json:"mustInclude,omitempty"`
	MustNotInclude []string         `yaml:"must_not_include" json:"mustNotInclude,omitempty"`
	// Turns make a follow-up case (#141): the dialogue after the analysis
	// is pinned, asked turn by turn along one chain of nodes.
	Turns []Turn `yaml:"turns" json:"turns,omitempty"`
}

// Turn is a question of a follow-up case: asked after the turn before it
// (the analysis for the first), in its node, or opening a node of the kind
// Fork on it, the turn the course links are looked for in.
type Turn struct {
	Fork           course.NodeKind `yaml:"fork" json:"fork,omitempty"`
	Ask            string          `yaml:"ask" json:"ask"`
	MustInclude    []string        `yaml:"must_include" json:"mustInclude,omitempty"`
	MustNotInclude []string        `yaml:"must_not_include" json:"mustNotInclude,omitempty"`
	// MustNotLink are concepts the turn a node forks with must not link
	// to: a name that only looks like one in the answer. There is no
	// must_link: what the reply names is up to the model.
	MustNotLink []string `yaml:"must_not_link" json:"mustNotLink,omitempty"`
}

// caseFile is a file of cases of one kind of course.
type caseFile struct {
	Category string `yaml:"category"`
	Cases    []Case `yaml:"cases"`
}

// LoadCases reads every *.yaml file in dir, in name order.
func LoadCases(dir string) ([]Case, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var out []Case
	seen := map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var cf caseFile
		if err := yaml.Unmarshal(raw, &cf); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, c := range cf.Cases {
			c.Category = cf.Category
			if err := c.check(); err != nil {
				return nil, fmt.Errorf("%s: case %q: %w", f, c.ID, err)
			}
			if seen[c.ID] {
				return nil, fmt.Errorf("%s: case %q is there twice", f, c.ID)
			}
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no cases in %s", dir)
	}
	return out, nil
}

func (c Case) check() error {
	var missing []string
	for name, v := range map[string]string{"id": c.ID, "version": c.Version, "lesson": c.Lesson, "quote": c.Quote} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	if c.Course <= 0 {
		missing = append(missing, "course")
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	if !assistant.ValidLength(c.Length) {
		return fmt.Errorf("length must be related or full")
	}
	for i, t := range c.Turns {
		if strings.TrimSpace(t.Ask) == "" {
			return fmt.Errorf("turn %d: missing ask", i+1)
		}
		if t.Fork != "" && !t.Fork.Openable() {
			return fmt.Errorf("turn %d: fork must be child, branch or divergent", i+1)
		}
		if t.Fork == "" && len(t.MustNotLink) > 0 {
			return fmt.Errorf("turn %d: must_not_link needs fork: only the turn a node forks with links", i+1)
		}
	}
	return nil
}
