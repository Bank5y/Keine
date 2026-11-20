package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Analysis is a valid answer: the three allParts of an exploration
// document that an analysis writes.
type Analysis struct {
	Title string `json:"title"`
	Intro string `json:"intro"`
	Body  string `json:"body"`
}

// Delta is a piece of a section as it streams in.
type Delta struct {
	Part Part   `json:"section"`
	Text string `json:"text"`
}

// Run error codes, as the reader's stream reports them.
const (
	CodeProviderError   = "provider_error"
	CodeRateLimited     = "rate_limited"
	CodeTimeout         = "timeout"
	CodeMalformedOutput = "malformed_output"
	CodeOutputTruncated = "output_truncated"
)

// RunError ends a run that could not finish. Kept tells whether the reader
// already saw some of the answer, which stays on screen.
type RunError struct {
	Code  string
	Kept  bool
	Cause error
}

func (e *RunError) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code
}

// Output tokens an analysis may write, with thinking off and on (#118).
const (
	maxTokens         = 4096
	maxTokensThinking = 8192
)

// remedyPrompt asks again, once, for an answer whose first try was not
// valid and not yet shown.
const remedyFormat = "上一次答复不合格：%s。请只按 <exp_title>/<exp_intro>/<exp_body> 格式重新作答，标签外不要写任何文字。"

// Analyze runs an analysis on the turn's model and passes each piece of
// the answer to delta as it streams. It returns the valid answer, a
// *RunError, or the context's error when the run was cancelled.
//
// A call that fails before anything streamed (rate limit, unavailable) is
// tried once more. An invalid answer that has not streamed is asked for
// once more with what was wrong; one that has streamed ends the run.
func Analyze(ctx context.Context, turn Turn, msgs []Message, delta func(Delta) error) (Analysis, error) {
	r := analysisRun{turn: turn, delta: delta}
	retried, remedied := false, false
	for {
		raw, p, res, err := r.call(ctx, msgs)
		if err != nil {
			if ctx.Err() != nil {
				return Analysis{}, ctx.Err()
			}
			var re *RunError
			if errors.As(err, &re) {
				return Analysis{}, err
			}
			c := CategoryOf(err)
			// 429, 5xx and connection errors (a connection timing out among them)
			if !r.streamed && !retried && (c == ErrRateLimited || c == ErrUnavailable || c == ErrTimeout) {
				retried = true
				continue
			}
			return Analysis{}, &RunError{Code: runCode(c), Kept: r.streamed, Cause: err}
		}
		problems := checkAnalysis(raw, p)
		if res.Finish == FinishLength {
			problems = append([]string{"答复太长，被截断了"}, problems...)
		}
		if len(problems) == 0 {
			return p.parts(), nil
		}
		if r.streamed || remedied {
			code := CodeMalformedOutput
			if res.Finish == FinishLength {
				code = CodeOutputTruncated
			}
			return Analysis{}, &RunError{Code: code, Kept: r.streamed, Cause: errors.New(strings.Join(problems, "; "))}
		}
		remedied = true
		msgs = append(msgs[:len(msgs):len(msgs)],
			Message{Role: RoleAssistant, Content: raw},
			Message{Role: RoleUser, Content: fmt.Sprintf(remedyFormat, strings.Join(problems, "；"))})
	}
}

type analysisRun struct {
	turn     Turn
	delta    func(Delta) error
	streamed bool
}

// deltaError wraps a failure to pass a delta on (the reader went away), so
// it is not taken for the model's.
type deltaError struct{ err error }

func (e deltaError) Error() string { return e.err.Error() }

// call makes one model call, streaming the tagged allParts as it goes.
func (r *analysisRun) call(ctx context.Context, msgs []Message) (string, *tagParser, Result, error) {
	var raw strings.Builder
	p := newTagParser(func(s Part, text string) error {
		r.streamed = true
		if err := r.delta(Delta{Part: s, Text: text}); err != nil {
			return deltaError{err}
		}
		return nil
	})
	max := maxTokens
	if r.turn.Thinking {
		max = maxTokensThinking
	}
	res, err := r.turn.Adapter.Stream(ctx, Request{Model: r.turn.Model, Messages: msgs, MaxTokens: max}, func(e Event) error {
		if e.Kind != EventText {
			return nil
		}
		raw.WriteString(e.Text)
		return p.feed(e.Text)
	})
	var de deltaError
	if errors.As(err, &de) {
		return "", nil, res, de.err
	}
	return raw.String(), p, res, err
}

func runCode(c Category) string {
	switch c {
	case ErrRateLimited:
		return CodeRateLimited
	case ErrTimeout:
		return CodeTimeout
	}
	return CodeProviderError
}
