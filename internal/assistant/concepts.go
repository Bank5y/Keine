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

// ConceptIndexPrompt is the prompt of extracting a lesson's summary and
// concepts for the concept index (#158, #224). It answers in JSON, not to
// a reader, so no answer style goes with it. An index records the version
// it was extracted with: one of another version is stale.
var ConceptIndexPrompt = Prompt{Version: "concept-index-2", System: strings.TrimSpace(conceptIndexSystem)}

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
// lesson's, or "" for none; Role tells whether the lesson defines it.
type Concept struct {
	Name    string      `json:"name"`
	Aliases []string    `json:"aliases"`
	Section string      `json:"section"`
	Role    ConceptRole `json:"role"`
	Summary string      `json:"summary"`
}

// ConceptRole is what a lesson does with a concept.
type ConceptRole string

const (
	// RoleDefine: the lesson explains or defines the concept.
	RoleDefine ConceptRole = "define"
	// RoleUse: the lesson uses a concept explained elsewhere.
	RoleUse ConceptRole = "use"
)

// Extraction is what an extraction found in a lesson: its summary and
// its concepts.
type Extraction struct {
	Summary  string
	Concepts []Concept
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
// it reads), its reply a JSON object of the lesson's summary and concepts
// (a bare array of concepts is taken too), continued when cut off at the
// model's max output (#169). A reply that is neither is a *RunError of
// CodeMalformedOutput. The meta is what it used, for the site to be
// charged, on failure too.
func ExtractConcepts(ctx context.Context, run Run, l ConceptLesson) (Extraction, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	// each call of a long output has the deadline's time to write
	run.Deadline *= 1 + MaxContinues
	run.Tools, run.Web = nil, nil
	run.Turn.ProviderWeb, run.Turn.Web = false, nil
	discard := Sink{Delta: func(Delta) error { return nil }, Activity: func(Activity) error { return nil }}
	r := newAnalysisRun(run, discard)
	r.plain, r.long = true, true
	c, err := r.loop(ctx, l.Messages())
	if err != nil {
		return Extraction{}, r.meta(), err
	}
	x, err := parseExtraction(c.raw)
	if err != nil {
		return Extraction{}, r.meta(), &RunError{Code: CodeMalformedOutput, Cause: err}
	}
	return x, r.meta(), nil
}

// parseExtraction reads the reply's object, or its bare array of
// concepts, in a code block or not.
func parseExtraction(reply string) (Extraction, error) {
	s := strings.TrimSpace(reply)
	obj, arr := strings.Index(s, "{"), strings.Index(s, "[")
	if obj < 0 || arr >= 0 && arr < obj {
		concepts, err := parseConcepts(s)
		return Extraction{Concepts: concepts}, err
	}
	if j := strings.LastIndex(s, "}"); j > obj {
		s = s[obj : j+1]
	}
	var raw struct {
		Summary  string          `json:"summary"`
		Concepts json.RawMessage `json:"concepts"`
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Extraction{}, errors.New("the extraction is not a JSON object: " + err.Error())
	}
	if len(raw.Concepts) == 0 {
		return Extraction{}, errors.New("the extraction has no concepts list")
	}
	concepts, err := parseConcepts(string(raw.Concepts))
	return Extraction{Summary: strings.TrimSpace(raw.Summary), Concepts: concepts}, err
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
		if c.Role != RoleUse {
			c.Role = RoleDefine
		}
		out = append(out, c)
	}
	return out, nil
}
