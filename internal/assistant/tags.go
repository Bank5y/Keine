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

func openTag(s Part) string  { return "<exp_" + string(s) + ">" }
func closeTag(s Part) string { return "</exp_" + string(s) + ">" }

// tagParser splits a streamed answer into its tagged parts character by
// character (ADR 0008): text outside the tags is dropped, a tag cut between
// two chunks is held back until it is complete, and whitespace at either
// end of a section is never passed on. What it passes on to emit is exactly
// what it keeps as the section's text.
type tagParser struct {
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
	return &tagParser{emit: emit, text: map[Part]*strings.Builder{}, closed: map[Part]bool{}}
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
				p.buf = p.buf[len(openTag(sec)):]
				p.enter(sec)
				continue
			}
			if p.mayOpen() {
				return nil
			}
			p.buf = p.buf[1:]
			continue
		}
		end := closeTag(p.cur)
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
	for _, s := range allParts {
		if strings.HasPrefix(p.buf, openTag(s)) {
			return s, true
		}
	}
	return "", false
}

// mayOpen tells whether the buffer may still grow into an opening tag.
func (p *tagParser) mayOpen() bool {
	for _, s := range allParts {
		if strings.HasPrefix(openTag(s), p.buf) {
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

// parts is what each closed section holds.
func (p *tagParser) parts() Analysis {
	get := func(s Part) string {
		if b := p.text[s]; b != nil {
			return b.String()
		}
		return ""
	}
	return Analysis{Title: get(PartTitle), Concept: get(PartConcept), Explanation: get(PartExplanation)}
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
