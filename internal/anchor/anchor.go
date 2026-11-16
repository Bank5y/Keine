// Package anchor finds annotations in lesson text by the quoted text and a
// little of the text around it (ADR-0003), never by offsets into a document
// that a course update may rewrite.
//
// Text is compared with all whitespace removed: the reader's rendered page
// and the Markdown source disagree on line breaks and spacing, but not on the
// visible characters. A Range therefore counts non-whitespace characters
// (Unicode code points) of the text, which the reader maps onto its page the
// same way.
package anchor

import "unicode"

// Quote is how an annotation remembers its place: the selected text and a
// short stretch of text before and after it.
type Quote struct {
	Exact  string
	Prefix string
	Suffix string
}

// Range is a stretch of a Text: [Start, End) in non-whitespace characters.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Text is a lesson's visible text, prepared for locating quotes in.
type Text struct {
	chars []rune
	// raw is the text with its whitespace; at[i] is where chars[i] starts in it
	raw string
	at  []int
}

// NewText prepares plain text.
func NewText(s string) Text {
	t := Text{chars: make([]rune, 0, len(s)), raw: s, at: make([]int, 0, len(s))}
	for i, r := range s {
		if !isSpace(r) {
			t.chars = append(t.chars, r)
			t.at = append(t.at, i)
		}
	}
	return t
}

// isSpace is whitespace as both the server and the reader skip it.
func isSpace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }

func visible(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !isSpace(r) {
			out = append(out, r)
		}
	}
	return out
}

// Locate finds where a quote sits in the text. Among several places that
// hold the quoted text it picks the one whose surroundings agree best with
// the quote's prefix and suffix, the earliest on a tie. It reports false
// when the text does not hold the quote at all.
func (t Text) Locate(q Quote) (Range, bool) {
	exact := visible(q.Exact)
	if len(exact) == 0 {
		return Range{}, false
	}
	prefix, suffix := visible(q.Prefix), visible(q.Suffix)
	best, bestScore := Range{}, -1
	for i := 0; i+len(exact) <= len(t.chars); i++ {
		if !equal(t.chars[i:i+len(exact)], exact) {
			continue
		}
		end := i + len(exact)
		score := commonSuffix(t.chars[:i], prefix) + commonPrefix(t.chars[end:], suffix)
		if score > bestScore {
			best, bestScore = Range{Start: i, End: end}, score
		}
	}
	return best, bestScore >= 0
}

// Relocate finds a quote again in a text that changed since the quote was
// taken, reporting false unless it can put it back with confidence: the
// place must hold the quoted text with at least one side of its
// surroundings intact (so an edit next to it on one side is tolerated, but
// another place that merely holds the same words is not), and agree with
// the surroundings better than any other such place.
func (t Text) Relocate(q Quote) (Range, bool) {
	exact := visible(q.Exact)
	if len(exact) == 0 {
		return Range{}, false
	}
	prefix, suffix := visible(q.Prefix), visible(q.Suffix)
	best, bestScore, tied := Range{}, -1, false
	for i := 0; i+len(exact) <= len(t.chars); i++ {
		if !equal(t.chars[i:i+len(exact)], exact) {
			continue
		}
		end := i + len(exact)
		before, after := commonSuffix(t.chars[:i], prefix), commonPrefix(t.chars[end:], suffix)
		if before < len(prefix) && after < len(suffix) {
			continue
		}
		switch score := before + after; {
		case score > bestScore:
			best, bestScore, tied = Range{Start: i, End: end}, score, false
		case score == bestScore:
			tied = true
		}
	}
	return best, bestScore >= 0 && !tied
}

func equal(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// commonPrefix is how many leading characters a and b share.
func commonPrefix(a, b []rune) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// commonSuffix is how many trailing characters a and b share.
func commonSuffix(a, b []rune) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// String is the visible text, whitespace removed.
func (t Text) String() string { return string(t.chars) }
