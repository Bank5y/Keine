package assistant

import (
	"errors"
	"strings"
)

// Part is one of the three parts of an analysis (ADR 0012): title,
// concept and concept explanation.
type Part string

const (
	PartTitle       Part = "title"
	PartConcept     Part = "concept"
	PartExplanation Part = "explanation"
)

var allParts = []Part{PartTitle, PartConcept, PartExplanation}

func openTag(s Part) string  { return analysisForm.open(s) }
func closeTag(s Part) string { return analysisForm.close(s) }

// tagForm is the shape of a tagged answer (ADR 0008): the prefix of its
// tags and its parts, how a finished one is checked and told to answer
// again, and how much it may write.
type tagForm struct {
	prefix string
	parts  []Part
	// check lists what is wrong with a finished answer; none means valid
	check func(raw string, p *tagParser) []string
	// remedy asks for the answer again, %s being what was wrong
	remedy string
	// output tokens it may write, with thinking off and on
	maxTokens, maxThinking int
}

func (f *tagForm) open(s Part) string  { return "<" + f.prefix + string(s) + ">" }
func (f *tagForm) close(s Part) string { return "</" + f.prefix + string(s) + ">" }

// analysisForm is the analysis's: three parts in exp_ tags.
var analysisForm = &tagForm{prefix: "exp_", parts: allParts, remedy: remedyFormat, maxTokens: maxTokens, maxThinking: maxTokensThinking}

func init() { analysisForm.check = checkAnalysis }

// tagParser splits a streamed answer into its tagged parts character by
// character (ADR 0008): text outside the tags is dropped, a tag cut between
// two chunks is held back until it is complete, and whitespace at either
// end of a section is never passed on. What it passes on to emit is exactly
// what it keeps as the section's text.
type tagParser struct {
	form *tagForm
	emit func(Part, string) error
	buf  string
	// cur is the section being read, "" outside the tags
	cur Part
	// discard: cur is a section met before, read only to find its end
	discard bool
	// started: cur has passed on text; ws is whitespace held back after it
	started bool
	ws      string
	text    map[Part]*strings.Builder
	closed  map[Part]bool
	// repeated: a section was opened a second time
	repeated bool
	// emitted: some text was passed on
	emitted bool
}

func newTagParser(emit func(Part, string) error) *tagParser {
	return newFormParser(analysisForm, emit)
}

func newFormParser(form *tagForm, emit func(Part, string) error) *tagParser {
	return &tagParser{form: form, emit: emit, text: map[Part]*strings.Builder{}, closed: map[Part]bool{}}
}

func (p *tagParser) feed(s string) error {
	p.buf += s
	for p.buf != "" {
		i := strings.IndexByte(p.buf, '<')
		if p.cur == "" {
			if i < 0 {
				p.buf = ""
				return nil
			}
			p.buf = p.buf[i:]
			if sec, ok := p.opening(); ok {
				p.buf = p.buf[len(p.form.open(sec)):]
				p.enter(sec)
				continue
			}
			if p.mayOpen() {
				return nil
			}
			p.buf = p.buf[1:]
			continue
		}
		end := p.form.close(p.cur)
		if i < 0 {
			err := p.write(p.buf)
			p.buf = ""
			return err
		}
		if err := p.write(p.buf[:i]); err != nil {
			return err
		}
		p.buf = p.buf[i:]
		switch {
		case strings.HasPrefix(p.buf, end):
			p.buf = p.buf[len(end):]
			if !p.discard {
				p.closed[p.cur] = true
			}
			p.cur, p.discard, p.started, p.ws = "", false, false, ""
		case strings.HasPrefix(end, p.buf):
			return nil // the closing tag may be cut here
		default:
			if err := p.write("<"); err != nil {
				return err
			}
			p.buf = p.buf[1:]
		}
	}
	return nil
}

// opening reports the section whose opening tag starts the buffer.
func (p *tagParser) opening() (Part, bool) {
	for _, s := range p.form.parts {
		if strings.HasPrefix(p.buf, p.form.open(s)) {
			return s, true
		}
	}
	return "", false
}

// mayOpen tells whether the buffer may still grow into an opening tag.
func (p *tagParser) mayOpen() bool {
	for _, s := range p.form.parts {
		if strings.HasPrefix(p.form.open(s), p.buf) {
			return true
		}
	}
	return false
}

func (p *tagParser) enter(s Part) {
	p.cur = s
	if _, seen := p.text[s]; seen {
		p.discard, p.repeated = true, true
		return
	}
	p.text[s] = &strings.Builder{}
}

// write passes on text of the current section, holding back whitespace at
// its start and, until more text follows, at its end.
func (p *tagParser) write(s string) error {
	if p.discard || s == "" {
		return nil
	}
	if !p.started {
		s = strings.TrimLeft(s, " \t\r\n")
		if s == "" {
			return nil
		}
	}
	s = p.ws + s
	trimmed := strings.TrimRight(s, " \t\r\n")
	p.ws = s[len(trimmed):]
	if trimmed == "" {
		return nil
	}
	p.started = true
	p.text[p.cur].WriteString(trimmed)
	p.emitted = true
	return p.emit(p.cur, trimmed)
}

// get is what a section holds.
func (p *tagParser) get(s Part) string {
	if b := p.text[s]; b != nil {
		return b.String()
	}
	return ""
}

// parts is what each section of an analysis holds.
func (p *tagParser) parts() Analysis {
	return Analysis{Title: p.get(PartTitle), Concept: p.get(PartConcept), Explanation: p.get(PartExplanation)}
}

// ErrAnalysisExpired: the analysis to pin is not there to pin: more than
// half an hour old, replaced by a newer one, or another reader's or
// another lesson's (#123).
var ErrAnalysisExpired = errors.New("analysis expired or replaced")

// Reply is the answer as the model's final reply writes it: the three
// parts in their tags, which is how a later turn reads the first one.
func (a Analysis) Reply() string {
	return openTag(PartTitle) + a.Title + closeTag(PartTitle) + "\n" +
		openTag(PartConcept) + a.Concept + closeTag(PartConcept) + "\n" +
		openTag(PartExplanation) + "\n" + a.Explanation + "\n" + closeTag(PartExplanation)
}
