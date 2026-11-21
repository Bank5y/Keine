package assistant

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

//go:embed prompts/concept_index.md
var conceptIndexSystem string

// ConceptIndexPrompt is the prompt of extracting a lesson's concepts for
// the concept index (#158). It answers in JSON, not to a reader, so no
// answer style goes with it.
var ConceptIndexPrompt = Prompt{Version: "concept-index-1", System: strings.TrimSpace(conceptIndexSystem)}

// ConceptLesson is the lesson an extraction reads: the course it is in,
// where, its sections' headings and its text.
type ConceptLesson struct {
	CourseTitle string
	Chapter     string
	Title       string
	Sections    []string
	Markdown    string
}

// Concept is a concept the extraction found: Section is a heading of the
// lesson's, or "" for none.
type Concept struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Section string   `json:"section"`
	Summary string   `json:"summary"`
}

// Messages is the request of an extraction.
func (l ConceptLesson) Messages() []Message {
	var b strings.Builder
	b.WriteString(headCourseInfo + "\n课程：" + l.CourseTitle)
	b.WriteString("\n\n## 这一讲\n" + l.Chapter + " › " + l.Title)
	b.WriteString("\n\n## 小节目录")
	if len(l.Sections) == 0 {
		b.WriteString("\n（没有小节）")
	}
	for _, s := range l.Sections {
		b.WriteString("\n- " + s)
	}
	b.WriteString("\n\n## 全文\n" + strings.TrimSpace(l.Markdown))
	return []Message{{Role: RoleSystem, Content: ConceptIndexPrompt.System}, {Role: RoleUser, Content: b.String()}}
}

// ExtractConcepts runs one extraction of the concept index on the turn's
// model: the analysis's loop with no tools and no web (the lesson is all
// it reads), its reply a JSON array of concepts. A reply that is not one
// is a *RunError of CodeMalformedOutput. The meta is what it used, for
// the site to be charged, on failure too.
func ExtractConcepts(ctx context.Context, run Run, l ConceptLesson) ([]Concept, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	run.Tools, run.Web = nil, nil
	run.Turn.ProviderWeb, run.Turn.Web = false, nil
	discard := Sink{Delta: func(Delta) error { return nil }, Activity: func(Activity) error { return nil }}
	r := newAnalysisRun(run, discard)
	r.plain = true
	c, err := r.loop(ctx, l.Messages())
	if err != nil {
		return nil, r.meta(), err
	}
	concepts, err := parseConcepts(c.raw)
	if err != nil {
		return nil, r.meta(), &RunError{Code: CodeMalformedOutput, Cause: err}
	}
	return concepts, r.meta(), nil
}

// parseConcepts reads the reply's array, in a code block or not, and
// leaves out items without a name or a summary.
func parseConcepts(reply string) ([]Concept, error) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.LastIndex(s, "]"); j > i {
			s = s[i : j+1]
		}
	}
	var raw []Concept
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, errors.New("concepts are not a JSON array: " + err.Error())
	}
	out := []Concept{}
	for _, c := range raw {
		c.Name, c.Summary, c.Section = strings.TrimSpace(c.Name), strings.TrimSpace(c.Summary), strings.TrimSpace(c.Section)
		if c.Name == "" || c.Summary == "" {
			continue
		}
		var aliases []string
		for _, a := range c.Aliases {
			if a = strings.TrimSpace(a); a != "" && a != c.Name {
				aliases = append(aliases, a)
			}
		}
		c.Aliases = aliases
		out = append(out, c)
	}
	return out, nil
}
