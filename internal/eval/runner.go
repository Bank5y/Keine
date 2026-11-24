package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Bank5y/keine/internal/anchor"
	"github.com/Bank5y/keine/internal/assistant"
	"github.com/Bank5y/keine/internal/course"
	"github.com/Bank5y/keine/internal/credits"
	"github.com/Bank5y/keine/internal/lessonctx"
)

// Stats are the measured, not judged, figures of a run.
type Stats struct {
	// Steps are the calls that made tool calls; ToolCalls the calls of
	// tools they made; Reread whether the run read the selection's own
	// lesson again, which the prompt asks it not to.
	Steps     int  `json:"steps"`
	ToolCalls int  `json:"toolCalls"`
	Reread    bool `json:"reread"`
	// Remedies are the calls that asked again for a valid answer.
	Remedies int `json:"remedies"`
	// DurationMs is the whole run; FirstTextMs until the reader would see
	// the first character (-1 when nothing streamed).
	DurationMs  int64 `json:"durationMs"`
	FirstTextMs int64 `json:"firstTextMs"`
	// LessonText is how much of the lesson went in: full or related.
	LessonText string          `json:"lessonText"`
	Usage      assistant.Usage `json:"usage"`
	Estimated  bool            `json:"estimated,omitempty"`
	Searches   int             `json:"searches"`
	// Credits are what the run would have been charged at the model's
	// rates (0 for a model without rates).
	Credits float64 `json:"credits"`
}

// Result is one run of a case: the answer in full, the hard rules it
// broke and its figures.
type Result struct {
	Case    string `json:"case"`
	Attempt int    `json:"attempt"`
	// Raw is what the model's last call wrote; Analysis its three parts.
	Raw      string             `json:"raw"`
	Analysis assistant.Analysis `json:"analysis"`
	// Error is the run's error code, "" when it answered.
	Error    string                `json:"error,omitempty"`
	Failures []Failure             `json:"failures"`
	Stats    Stats                 `json:"stats"`
	Sources  []assistant.WebSource `json:"sources"`
	// Turns are the turns of a follow-up case that were asked: all of
	// them, unless the analysis or a turn did not end with an answer.
	Turns []TurnResult `json:"turns,omitempty"`
}

// Hard tells whether the run broke any hard rule.
func (r Result) Hard() bool { return len(r.Failures) > 0 }

// Runner runs the analysis of cases the way a reader's analysis runs, on
// the model Turn gives, and touches nothing: it pins nothing, records no
// usage and signs in nowhere.
type Runner struct {
	Courses *course.Service
	// Turn is the model each run takes: the site's analysis model, or one
	// given on the command line.
	Turn func(context.Context) (assistant.Turn, error)
	// Followup is the model the turns after the analysis take (default
	// Turn): the site's follow-up model, or the one given.
	Followup func(context.Context) (assistant.Turn, error)
	Mermaid  MermaidParser
	// Deadline ends a run (default assistant.DefaultDeadline).
	Deadline time.Duration
	Now      func() time.Time
}

// Run runs a case once. err is a failure of keine-eval itself (the
// database gone); what goes wrong with the case is in the result.
func (r *Runner) Run(ctx context.Context, c Case, attempt int) (Result, error) {
	res := Result{Case: c.ID, Attempt: attempt, Failures: []Failure{}, Sources: []assistant.WebSource{}}
	stale := func(format string, args ...any) (Result, error) {
		res.Failures = append(res.Failures, Failure{Rule: RuleCase, Detail: fmt.Sprintf(format, args...)})
		return res, nil
	}
	viewer, err := r.Courses.Uploader(ctx, c.Course)
	if errors.Is(err, course.ErrNotFound) {
		return stale("课程 %d 不存在", c.Course)
	}
	if err != nil {
		return res, err
	}
	detail, err := r.Courses.Get(ctx, viewer, c.Course)
	if err != nil {
		return stale("课程 %d 读不到: %v", c.Course, err)
	}
	if detail.FilesDir != c.Version {
		return stale("课程 %d 已更新到版本 %s, 用例写于 %s", c.Course, detail.FilesDir, c.Version)
	}
	lessonID, ok := detail.LessonID(c.Lesson)
	if !ok {
		return stale("课程 %d 没有讲 %s", c.Course, c.Lesson)
	}
	p, err := r.Courses.Passage(ctx, viewer, c.Course, lessonID, anchor.Quote{Exact: c.Quote, Prefix: c.Prefix, Suffix: c.Suffix})
	if err != nil {
		return stale("选区在讲 %s 里找不到: %v", c.Lesson, err)
	}

	turn, err := r.Turn(ctx)
	if err != nil {
		return res, err
	}
	msgs, related, err := lessonctx.AnalysisMessages(p, c.Quote, c.Length, assistant.LimitsOf(turn))
	var choice *assistant.LengthChoiceError
	switch {
	case errors.As(err, &choice):
		return stale("这一讲超过了软阈值 (%d > %d token), 用例要写 length: related 或 full", choice.Lesson, choice.Soft)
	case err != nil:
		return stale("上下文: %v", err)
	}
	res.Stats.LessonText = "full"
	if related {
		res.Stats.LessonText = "related"
	}

	run, w := r.watched(turn, c.Lesson, assistant.CourseTools(lessonctx.NewLibrary(r.Courses, viewer, p.Course)))
	a, meta, err := assistant.Analyze(ctx, run, msgs, w.sink())
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	res.Analysis, res.Raw, res.Sources = a, w.rec.last(), meta.Sources
	res.Stats = w.stats(meta, turn.Rates, res.Stats)
	res.Error = errorCode(err)
	switch res.Error {
	case "", assistant.CodeMalformedOutput, assistant.CodeOutputTruncated:
		// an answer that came: judged by the rules, which name what is wrong
		if res.Analysis == (assistant.Analysis{}) {
			res.Analysis, _ = assistant.CheckAnalysis(res.Raw)
		}
		res.Failures = append(res.Failures, Check(res.Raw, c, p.Lesson.Markdown, r.Mermaid)...)
		if res.Error == assistant.CodeOutputTruncated {
			res.Failures = append(res.Failures, Failure{Rule: RuleServer, Detail: "答复太长, 被截断了"})
		}
	default:
		res.Failures = append(res.Failures, Failure{Rule: RuleRun, Detail: why(res.Error, err)})
	}
	if len(c.Turns) == 0 || res.Error != "" {
		// an analysis that did not come out whole cannot be pinned
		return res, nil
	}
	pinned := assistant.FollowupContext{Snapshot: msgs, FirstQuestion: assistant.FirstQuestion(c.Quote, res.Analysis.Title),
		// kept as the server pins it: its three parts, whatever calls wrote them
		FirstReply: res.Analysis.Reply()}
	return res, r.dialogue(ctx, &res, c, pinned, lessonctx.NewLibrary(r.Courses, viewer, p.Course))
}

// why is what ended a run that did not answer: its code, and the error.
func why(code string, err error) string {
	if err != nil {
		return code + ": " + err.Error()
	}
	return code
}

// watch is a run as keine-eval measures it: its model's calls, and when
// the reader would see the first character.
type watch struct {
	rec   *recorder
	now   func() time.Time
	start time.Time
	first time.Time
}

// watched is the run of a turn on its model, with the course tools, its
// calls recorded; lesson is the selection's.
func (r *Runner) watched(turn assistant.Turn, lesson string, tools []assistant.Tool) (assistant.Run, *watch) {
	now := r.Now
	if now == nil {
		now = time.Now
	}
	w := &watch{rec: &recorder{Adapter: turn.Adapter, lesson: lesson}, now: now}
	turn.Adapter = w.rec
	w.start = now()
	return assistant.Run{Turn: turn, Tools: tools, Web: turn.Web, Now: now, Deadline: r.Deadline}, w
}

func (w *watch) sink() assistant.Sink {
	return assistant.Sink{
		Delta: func(d assistant.Delta) error {
			if w.first.IsZero() && d.Text != "" {
				w.first = w.now()
			}
			return nil
		},
		Activity: func(assistant.Activity) error { return nil },
	}
}

// stats are the run's figures, on s (which may say how much of the lesson
// went in).
func (w *watch) stats(meta assistant.Meta, rates assistant.Rates, s Stats) Stats {
	s.Steps, s.Remedies = meta.Steps, meta.Remedies
	s.ToolCalls, s.Reread = w.rec.counts()
	s.DurationMs = w.now().Sub(w.start).Milliseconds()
	s.FirstTextMs = -1
	if !w.first.IsZero() {
		s.FirstTextMs = w.first.Sub(w.start).Milliseconds()
	}
	s.Usage, s.Estimated, s.Searches = meta.Usage, meta.Estimated, meta.Searches
	s.Credits = credits.Cost(meta.Usage, meta.Searches, rates)
	return s
}

// errorCode is a run's error code, "" when it answered.
func errorCode(err error) string {
	var re *assistant.RunError
	switch {
	case errors.As(err, &re):
		return re.Code
	case err != nil:
		return assistant.CodeProviderError
	}
	return ""
}

// recorder is the model of a run as keine-eval watches it: each call's
// text, the tool calls made, and the entries a linking step named.
type recorder struct {
	assistant.Adapter
	lesson string
	mu     sync.Mutex
	texts  []string
	calls  int
	reread bool
	linked []string
}

func (r *recorder) Stream(ctx context.Context, req assistant.Request, emit func(assistant.Event) error) (assistant.Result, error) {
	var text strings.Builder
	res, err := r.Adapter.Stream(ctx, req, func(e assistant.Event) error {
		switch e.Kind {
		case assistant.EventText:
			text.WriteString(e.Text)
		case assistant.EventToolCall:
			r.toolCall(e.ToolCall)
		}
		return emit(e)
	})
	r.mu.Lock()
	r.texts = append(r.texts, text.String())
	r.mu.Unlock()
	return res, err
}

func (r *recorder) toolCall(c *assistant.ToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.Name == assistant.ToolLinkCourse {
		// the linking step is not one of the answer's tool calls
		var args struct {
			Links []struct {
				Entry string `json:"entry"`
			} `json:"links"`
		}
		_ = json.Unmarshal([]byte(c.Arguments), &args)
		for _, l := range args.Links {
			r.linked = append(r.linked, l.Entry)
		}
		return
	}
	r.calls++
	if c.Name != assistant.ToolReadLesson {
		return
	}
	var args struct {
		Lesson string `json:"lesson"`
	}
	if json.Unmarshal([]byte(c.Arguments), &args) == nil && args.Lesson == r.lesson {
		r.reread = true
	}
}

// last is what the last call wrote.
func (r *recorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.texts) == 0 {
		return ""
	}
	return r.texts[len(r.texts)-1]
}

func (r *recorder) counts() (calls int, reread bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.reread
}

// linkedEntries are the entries the linking step named, as it named them.
func (r *recorder) linkedEntries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.linked)
}
