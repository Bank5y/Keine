package assistant

import (
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"
)

// Length is how much of a long lesson the reader chose to send (#131).
type Length string

const (
	// LengthUnset: no choice, which a lesson past the soft limit needs.
	LengthUnset Length = ""
	// LengthRelated: the section of the selection, the ones next to it,
	// and the lesson's outline of sections.
	LengthRelated Length = "related"
	// LengthFull: the whole lesson, slower and dearer.
	LengthFull Length = "full"
)

func ValidLength(l Length) bool {
	return l == LengthUnset || l == LengthRelated || l == LengthFull
}

// Limits are how many tokens of a lesson a turn's model takes (#131).
// Hard is the context window less the answer and a quarter for tool
// results; past Soft, half of it, the reader chooses how much to send.
// Input is what the request may hold before the answer.
type Limits struct {
	Soft  int `json:"soft"`
	Hard  int `json:"hard"`
	Input int `json:"-"`
}

// ErrContextTooLong: what a run would send is past the model's window,
// or the whole of a lesson past its hard limit was chosen.
var ErrContextTooLong = errors.New("input past the context window")

// LengthChoiceError is a lesson past the soft limit, sent with no choice
// of how much of it to send: the limits and the lesson in tokens, its
// length in characters, and whether the whole of it still fits.
type LengthChoiceError struct {
	Limits
	Lesson int  `json:"lesson"`
	Chars  int  `json:"chars"`
	Full   bool `json:"full"`
}

func (e *LengthChoiceError) Error() string {
	return fmt.Sprintf("lesson of %d tokens past the soft limit of %d needs a length choice", e.Lesson, e.Soft)
}

// ChooseLength tells, for a lesson and the reader's choice, whether to
// send only the sections around the selection: a lesson within the soft
// limit goes whole whatever the choice; past it, a choice is needed
// (*LengthChoiceError), and the whole lesson only fits within the hard
// limit (ErrContextTooLong).
func ChooseLength(lesson string, choice Length, l Limits) (related bool, err error) {
	size := EstimateTokens(lesson)
	if size <= l.Soft {
		return false, nil
	}
	switch choice {
	case LengthUnset:
		return false, &LengthChoiceError{Limits: l, Lesson: size, Chars: utf8.RuneCountInString(lesson), Full: size <= l.Hard}
	case LengthFull:
		if size > l.Hard {
			return false, ErrContextTooLong
		}
		return false, nil
	}
	return true, nil
}

// LimitsOf are the limits of a turn's model.
func LimitsOf(t Turn) Limits {
	window := t.ContextWindow
	if window <= 0 {
		window = DefaultContextWindow
	}
	out := maxTokens
	if t.Thinking {
		out = maxTokensThinking
	}
	hard := max(window-out-window/4, 0)
	return Limits{Soft: hard / 2, Hard: hard, Input: window - out}
}

// EstimateTokens is about how many tokens s takes, without a tokenizer:
// one a character for CJK and the like, one per four bytes otherwise. It
// errs high for most text, which keeps a run inside the window.
func EstimateTokens(s string) int {
	n, other := 0, 0
	for _, r := range s {
		if r >= 0x2E80 {
			n++
		} else {
			other += utf8.RuneLen(r)
		}
	}
	return n + (other+3)/4
}

// EstimateMessages is about how many tokens the messages take.
func EstimateMessages(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateTokens(m.Content) + 4
	}
	return n
}

var (
	// ErrBusy: every run the deployment allows is taken.
	ErrBusy = errors.New("every run is taken")
	// ErrRunInProgress: the reader already has a run going.
	ErrRunInProgress = errors.New("the reader has a run in progress")
)

// DefaultMaxRuns is how many runs go at once unless the deployment says.
const DefaultMaxRuns = 16

// Gate keeps the runs going at once within the deployment's limit, and
// to one a reader (#118). A run that does not get in is turned away at
// once, never queued.
type Gate struct {
	mu      sync.Mutex
	max     int
	running int
	readers map[int64]bool
}

// NewGate lets max runs go at once (DefaultMaxRuns when max is not
// positive).
func NewGate(max int) *Gate {
	if max <= 0 {
		max = DefaultMaxRuns
	}
	return &Gate{max: max, readers: map[int64]bool{}}
}

// Enter lets the reader's run in, or says why not; leave ends it.
func (g *Gate) Enter(reader int64) (leave func(), err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.readers[reader] {
		return nil, ErrRunInProgress
	}
	if g.running >= g.max {
		return nil, ErrBusy
	}
	g.running++
	g.readers[reader] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.running--
			delete(g.readers, reader)
		})
	}, nil
}
