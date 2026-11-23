package assistant

import (
	"strings"
	"unicode/utf8"
)

// MaxContinues is how many times a long output cut off at the output cap
// is continued before the run gives up on it (#169).
const MaxContinues = 3

// The seam of a continuation: a model asked to go on often first repeats
// the end of what it wrote. The start of the continuation, up to
// seamWindow characters, is held back until it is known how much of it
// repeats; an overlap shorter than minOverlap is taken for chance, not a
// repetition.
const (
	seamWindow = 256
	minOverlap = 8
)

// seam joins a continuation to the reply it continues, dropping what of
// its start repeats the reply's end.
type seam struct {
	tail string
	held strings.Builder
	done bool
}

func newSeam(reply string) *seam { return &seam{tail: reply} }

// next takes a piece of the continuation and returns what of it goes on
// the reply: nothing while the start is held back.
func (s *seam) next(text string) string {
	if s.done {
		return text
	}
	s.held.WriteString(text)
	if utf8.RuneCountInString(s.held.String()) < seamWindow {
		return ""
	}
	return s.flush()
}

// flush returns what is held back, without the repetition, once the
// continuation ended (or the window is full).
func (s *seam) flush() string {
	if s.done {
		return ""
	}
	s.done = true
	h := s.held.String()
	return h[overlap(s.tail, h):]
}

// overlap is the length in bytes of the longest start of next that the
// end of prev repeats, if at least minOverlap characters long, else 0.
func overlap(prev, next string) int {
	best, runes := 0, 0
	for i := range next {
		if i > 0 {
			runes++
			if runes >= minOverlap && strings.HasSuffix(prev, next[:i]) {
				best = i
			}
		}
	}
	if utf8.RuneCountInString(next) >= minOverlap && strings.HasSuffix(prev, next) {
		best = len(next)
	}
	return best
}
