package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Analysis is a valid answer: the three allParts of an exploration
// document that an analysis writes.
type Analysis struct {
	Title       string `json:"title"`
	Concept     string `json:"concept"`
	Explanation string `json:"explanation"`
}

// Delta is a piece of a section as it streams in. One with Reset (and no
// text) throws away what streamed so far: the model wrote a draft, then
// went on to look something up.
type Delta struct {
	Part  Part   `json:"section"`
	Text  string `json:"text"`
	Reset bool   `json:"reset,omitempty"`
}

// Activity is a step of a run the reader sees in progress (ADR 0008): only
// its kind, target and status, and why it failed; the words are the front
// end's. Kind is a tool's name, or "thinking" while the model reasons (the
// reasoning itself never leaves the server).
type Activity struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Target string         `json:"target,omitempty"`
	Status ActivityStatus `json:"status"`
	Reason Category       `json:"reason,omitempty"`
	// OwnKey: a failed web call ran on the reader's own key, so the reader
	// is told why (no balance, a bad key) rather than only that it failed.
	OwnKey bool `json:"ownKey,omitempty"`
}

// Meta is what a run records in its turn's metadata besides the answer
// (ADR 0008): the source list, the capabilities that went off during the
// run, and the web requests the services bill.
type Meta struct {
	Sources []WebSource `json:"sources"`
	// Degraded are the web capabilities turned off during the run (an
	// account failure, two passing ones in a row): the reader is told the
	// run got no web results, which never goes into the document.
	Degraded []string `json:"degraded,omitempty"`
	Searches int      `json:"searches"`
	// Usage is what the run's model calls used together, Estimated set
	// when some of it was worked out locally (a call cut short, or one the
	// provider did not report); Charged is false when no call cost
	// anything (ADR 0010).
	Usage     Usage `json:"-"`
	Estimated bool  `json:"-"`
	Charged   bool  `json:"-"`
	// Steps are the run's calls that made tool calls; Remedies the calls
	// that asked again for an answer that was not valid. keine-eval
	// reports them.
	Steps    int `json:"-"`
	Remedies int `json:"-"`
}

type ActivityStatus string

const (
	ActivityThinking                = "thinking"
	ActivityRunning  ActivityStatus = "running"
	ActivityDone     ActivityStatus = "done"
	ActivityFailed   ActivityStatus = "failed"
)

// Sink is where a run sends what the reader sees while it goes. An error
// from either (the reader went away) stops the run.
type Sink struct {
	Delta    func(Delta) error
	Activity func(Activity) error
}

// Run error codes, as the reader's stream reports them.
const (
	CodeProviderError   = "provider_error"
	CodeRateLimited     = "rate_limited"
	CodeTimeout         = "timeout"
	CodeMalformedOutput = "malformed_output"
	CodeOutputTruncated = "output_truncated"
	// CodeOwnModelError: the reader's own model failed (ADR 0010); the run
	// does not fall back to the site's.
	CodeOwnModelError = "own_model_error"
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

// The fixed numbers of a run (#118).
const (
	// output tokens an analysis may write, with thinking off and on
	maxTokens         = 4096
	maxTokensThinking = 8192
	// MaxSteps is how many model calls with tools a run makes before the
	// one that has to answer.
	MaxSteps = 6
	// MaxParallel is how many tool calls of one step are carried out.
	MaxParallel = 5
	// DefaultDeadline ends a run; at three quarters of it (the soft
	// deadline) the run stops looking things up and answers.
	DefaultDeadline = 120 * time.Second
)

var (
	toolTimeout = 15 * time.Second
	// a model call that sends nothing for this long, at its start or in
	// the middle, failed to connect
	idleTimeout = 30 * time.Second
	// before the second try of a tool that failed for a while
	toolBackoff = 300 * time.Millisecond
)

// What the run tells the model along the way.
const (
	remedyFormat  = "上一次答复不合格：%s。请只按 <exp_title>/<exp_concept>/<exp_explanation> 格式重新作答，标签外不要写任何文字。"
	lastStepNote  = "还剩 1 次查询。之后必须直接作答。"
	stepsUsedNote = "查询次数已用完。请不要再调用工具，根据已有资料直接作答。"
	softNote      = "时间快到了。请不要再调用工具，根据已有资料直接作答。"
	tooManyCalls  = "本步最多 5 个工具调用，这个调用没有执行；需要的话请在下一步再调用。"
	internalNote  = "工具内部出错，这次调用没有结果。"
	transientNote = "这次调用暂时失败了，没有结果。"
	brokenFormat  = "本次运行%s不可用，请依据已有资料作答，不要编造来源。"
	unknownFormat = "没有名为 %s 的工具。可用的工具：%s。"
)

// Run is what an analysis runs with besides its messages.
type Run struct {
	Turn  Turn
	Tools []Tool
	// Web is how the run reaches the web on the Keine tool route; nil when
	// it does not (the turn's model searches no web, or searches it
	// itself). Its tools come after Tools.
	Web *WebRoute
	// Now tells the time the deadlines are kept by (default: the system
	// clock).
	Now func() time.Time
	// Deadline ends the run (default DefaultDeadline).
	Deadline time.Duration
	// Spent hears what the run has used so far, for a caller who stops the
	// run (by cancelling it) once it costs too much: after each model call,
	// and while a call writes, with that call worked out from its messages
	// and what it wrote so far.
	Spent func(Usage)
}

// errRunDeadline is what cancels a run whose time is up.
var errRunDeadline = errors.New("run deadline")

// Analyze runs an analysis on the turn's model and passes each piece of
// the answer to out as it streams, with an activity for each tool call and
// while the model reasons. It returns the valid answer, a *RunError, or
// the context's error when the run was cancelled.
//
// The loop (ADR 0008): the answer is the first call that makes no tool
// calls. A call that makes some has them carried out (at most MaxParallel,
// at once) and their results passed back for the next call. After MaxSteps
// such calls, or once the soft deadline passed, the run makes one call
// that keeps the tools declared but may not use them. A draft that
// streamed before tool calls is thrown away with a reset.
//
// The reasoning of a call goes back to the model with the run's later
// calls, as it was, and nowhere else.
//
// A call that fails before anything streamed (rate limit, unavailable) is
// tried once more. An invalid answer that has not streamed is asked for
// once more with what was wrong; one that has streamed ends the run.
//
// With a web route, web_search (and web_fetch) are among the tools; on the
// provider route, the calls declare the provider's own, and each of its
// steps is an activity too. The meta returned, on failure too, lists what
// either found.
func Analyze(ctx context.Context, run Run, msgs []Message, out Sink) (Analysis, Meta, error) {
	if run.Now == nil {
		run.Now = time.Now
	}
	if run.Deadline <= 0 {
		run.Deadline = DefaultDeadline
	}
	var web *Web
	if run.Web != nil {
		web = newWeb(*run.Web)
		run.Tools = append(slices.Clip(run.Tools), web.Tools()...)
	}
	r := newAnalysisRun(run, out)
	r.web = web
	c, err := r.loop(ctx, msgs)
	if err != nil {
		return Analysis{}, r.meta(), err
	}
	return c.parser.parts(), r.meta(), nil
}

// meta is what the run found, the searches billed (on the Keine tool
// route by the search services, on the provider route in the usage of the
// calls) and which capabilities went off.
func (r *analysisRun) meta() Meta {
	m := Meta{Sources: []WebSource{}}
	if r.web != nil {
		m.Sources, m.Searches = r.web.Sources(), r.web.Searches()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m.Searches += r.providerSearches
	m.Usage, m.Estimated, m.Charged = r.usage, r.estimated, r.charged
	m.Steps, m.Remedies = r.steps, r.remedies
	// a citation may name a page its search found under another title
	type place struct {
		url  string
		kind WebSourceKind
	}
	seen := map[place]bool{}
	for _, s := range m.Sources {
		seen[place{s.URL, s.Kind}] = true
	}
	for _, s := range r.provided {
		if p := (place{s.URL, s.Kind}); !seen[p] {
			seen[p] = true
			m.Sources = append(m.Sources, s)
		}
	}
	if r.web == nil {
		return m
	}
	for _, name := range []string{ToolWebSearch, ToolWebFetch} {
		if b, ok := r.breakers[name]; ok && b.off {
			m.Degraded = append(m.Degraded, name)
		}
	}
	return m
}

func (r *analysisRun) loop(ctx context.Context, msgs []Message) (called, error) {
	run := r.run
	runCtx, cancel := context.WithTimeoutCause(ctx, run.Deadline, errRunDeadline)
	defer cancel()
	specs := make([]ToolSpec, len(run.Tools))
	for i, t := range run.Tools {
		specs[i] = t.Spec()
	}
	tools := len(specs) > 0 || run.Turn.ProviderWeb
	answering := !tools // no more tool calls from here
	retried, remedied := false, false
	timeout := func(cause error) error {
		return &RunError{Code: CodeTimeout, Kept: r.streamed, Cause: cause}
	}
	for {
		if r.past(run.Deadline) {
			return called{}, timeout(errRunDeadline)
		}
		if !answering && r.past(run.Deadline*3/4) {
			answering = true
			msgs = appendMessages(msgs, Message{Role: RoleUser, Content: softNote})
		}
		req := Request{Model: run.Turn.Model, Messages: msgs, Tools: specs, MaxTokens: r.form.maxTokens, Thinking: run.Turn.Thinking, ProviderWeb: run.Turn.ProviderWeb}
		if run.Turn.Thinking {
			req.MaxTokens = r.form.maxThinking
		}
		if tools {
			req.ToolChoice = "auto"
			if answering {
				req.ToolChoice = "none"
			}
		}
		c, err := r.call(runCtx, req)
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return called{}, ctx.Err()
			case errors.Is(err, errRunDeadline) || errors.Is(context.Cause(runCtx), errRunDeadline):
				return called{}, timeout(err)
			}
			var re *RunError
			if errors.As(err, &re) {
				return called{}, err
			}
			cat := CategoryOf(err)
			// 429, 5xx and connection errors (a connection timing out among them)
			if !r.streamed && !retried && (cat == ErrRateLimited || cat == ErrUnavailable || cat == ErrTimeout) {
				retried = true
				continue
			}
			return called{}, &RunError{Code: runCode(cat), Kept: r.streamed, Cause: err}
		}
		if len(c.toolCalls) > 0 && !answering {
			r.steps++
			if r.streamed {
				if err := r.reset(); err != nil {
					return called{}, err
				}
			}
			msgs = appendMessages(msgs, Message{Role: RoleAssistant, Content: c.raw, Reasoning: c.reasoning, ToolCalls: c.toolCalls, Native: c.result.Native})
			msgs = append(msgs, r.execute(runCtx, c.toolCalls)...)
			if ctx.Err() != nil {
				return called{}, ctx.Err()
			}
			switch {
			case r.steps == MaxSteps-1:
				msgs = append(msgs, Message{Role: RoleUser, Content: lastStepNote})
			case r.steps >= MaxSteps:
				answering = true
				msgs = append(msgs, Message{Role: RoleUser, Content: stepsUsedNote})
			}
			continue
		}
		if r.plain {
			// a reply is Markdown as written: it is whole unless cut short
			switch {
			case c.result.Finish == FinishLength:
				return c, &RunError{Code: CodeOutputTruncated, Kept: r.streamed}
			case strings.TrimSpace(c.raw) == "":
				return c, &RunError{Code: CodeMalformedOutput, Kept: false, Cause: errors.New("empty reply")}
			}
			if r.linker != nil {
				// within the run's deadline, after the answer (ADR 0011)
				r.links = r.link(runCtx, msgs, c, r.linker)
			}
			return c, nil
		}
		problems := r.form.check(c.raw, c.parser)
		if c.result.Finish == FinishLength {
			problems = append([]string{"答复太长，被截断了"}, problems...)
		}
		if len(problems) == 0 {
			return c, nil
		}
		if r.streamed || remedied {
			code := CodeMalformedOutput
			if c.result.Finish == FinishLength {
				code = CodeOutputTruncated
			}
			return called{}, &RunError{Code: code, Kept: r.streamed, Cause: errors.New(strings.Join(problems, "; "))}
		}
		remedied = true
		r.remedies++
		answering = true
		msgs = appendMessages(msgs,
			Message{Role: RoleAssistant, Content: c.raw, Reasoning: c.reasoning, Native: c.result.Native},
			Message{Role: RoleUser, Content: fmt.Sprintf(r.form.remedy, strings.Join(problems, "；"))})
	}
}

// appendMessages appends to a copy, so the caller's slice is never written.
func appendMessages(msgs []Message, more ...Message) []Message {
	return append(msgs[:len(msgs):len(msgs)], more...)
}

type analysisRun struct {
	run   Run
	start time.Time
	tools map[string]Tool
	// mu keeps what goes to the reader in order while tools run at once,
	// and guards the breakers
	mu  sync.Mutex
	out Sink
	// streamed: the reader sees some of an answer
	streamed bool
	// plain: the answer is Markdown that streams as it is written (a
	// follow-up turn), not an analysis's tagged parts; text is what of it
	// the reader sees
	plain bool
	text  strings.Builder
	// linker, on the turn a node forks with, links the answer to the
	// course (ADR 0011); links are what it linked
	linker Linker
	links  []CourseLink
	// quiet: the call under way is the linking step's, which the reader
	// sees only as its activity
	quiet bool
	// form is the shape of a tagged answer (an analysis's by default)
	form *tagForm
	// calls and toolRuns number the activities
	calls, toolRuns int
	// steps are the calls that made tool calls; remedies the calls that
	// asked again for an answer that was not valid
	steps, remedies int
	breakers        map[string]*breaker
	// web is the run's web tools, nil without a web route
	web *Web
	// provided are the sources the provider's own search found, or the
	// answer cited, in order; providerSearches the searches it billed
	provided         []WebSource
	providerSearches int
	// usage is what the calls used so far; estimated, some of it worked
	// out locally; charged, some call cost anything
	usage              Usage
	estimated, charged bool
}

// breaker is how a tool fared in this run (#117): it is off after an
// account failure or two transient ones in a row.
type breaker struct {
	failures int
	off      bool
}

func newAnalysisRun(run Run, out Sink) *analysisRun {
	r := &analysisRun{run: run, start: run.Now(), tools: map[string]Tool{}, breakers: map[string]*breaker{}, form: analysisForm}
	for _, t := range run.Tools {
		r.tools[t.Spec().Name] = t
		r.breakers[t.Spec().Name] = &breaker{}
	}
	r.out = Sink{
		Delta: func(d Delta) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			return out.Delta(d)
		},
		Activity: func(a Activity) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			return out.Activity(a)
		},
	}
	return r
}

// past tells whether d has passed since the run started.
func (r *analysisRun) past(d time.Duration) bool { return r.run.Now().Sub(r.start) >= d }

func (r *analysisRun) reset() error {
	r.streamed = false
	r.text.Reset()
	return r.out.Delta(Delta{Reset: true})
}

// sinkError wraps a failure to pass something on to the reader (who went
// away), so it is not taken for the model's.
type sinkError struct{ err error }

func (e sinkError) Error() string { return e.err.Error() }

// called is what one model call wrote.
type called struct {
	raw, reasoning string
	parser         *tagParser
	toolCalls      []ToolCall
	result         Result
}

// call makes one model call, streaming the tagged parts as it goes.
func (r *analysisRun) call(ctx context.Context, req Request) (called, error) {
	var raw, reasoning strings.Builder
	var toolCalls []ToolCall
	r.calls++
	think := Activity{ID: fmt.Sprintf("thinking-%d", r.calls), Kind: ActivityThinking}
	activity := func(status ActivityStatus) error {
		think.Status = status
		if r.quiet {
			return nil
		}
		return r.out.Activity(think)
	}
	// while the call writes, what it costs so far is worked out locally
	var asked int64
	if r.run.Spent != nil {
		asked = int64(EstimateMessages(req.Messages))
	}
	p := newFormParser(r.form, func(s Part, text string) error {
		r.streamed = true
		if err := r.out.Delta(Delta{Part: s, Text: text}); err != nil {
			return sinkError{err}
		}
		return nil
	})
	res, err := r.stream(ctx, req, func(e Event) error {
		if r.past(r.run.Deadline) {
			return errRunDeadline
		}
		if ctx.Err() != nil {
			// cancelled while the model writes (the reader's credits ran
			// out): nothing more goes out
			return context.Cause(ctx)
		}
		switch e.Kind {
		case EventReasoning:
			if think.Status == "" {
				if err := activity(ActivityRunning); err != nil {
					return sinkError{err}
				}
			}
			reasoning.WriteString(e.Text)
		case EventText:
			if think.Status == ActivityRunning {
				if err := activity(ActivityDone); err != nil {
					return sinkError{err}
				}
			}
			raw.WriteString(e.Text)
			defer r.peek(asked, raw.String())
			if r.quiet {
				return nil
			}
			if r.plain {
				if e.Text == "" {
					return nil
				}
				r.streamed = true
				r.text.WriteString(e.Text)
				if err := r.out.Delta(Delta{Text: e.Text}); err != nil {
					return sinkError{err}
				}
				return nil
			}
			return p.feed(e.Text)
		case EventToolCall:
			toolCalls = append(toolCalls, *e.ToolCall)
		case EventWeb:
			r.addProvided(e.Sources)
			w := e.Web
			if w == nil {
				return nil
			}
			if err := r.out.Activity(Activity{ID: "web-" + w.ID, Kind: w.Kind, Target: w.Target, Status: w.Status, Reason: w.Reason}); err != nil {
				return sinkError{err}
			}
		case EventSources:
			r.addProvided(e.Sources)
		}
		return nil
	})
	r.spend(req, res.Usage, raw.String()+reasoning.String(), len(toolCalls) > 0)
	if se, ok := errors.AsType[sinkError](err); ok {
		return called{}, se.err
	}
	if err == nil && res.Usage.Searches != nil {
		r.mu.Lock()
		r.providerSearches += int(*res.Usage.Searches)
		r.mu.Unlock()
	}
	if err == nil && think.Status == ActivityRunning {
		// it thought to the end: the activity ends with the call
		if aerr := activity(ActivityDone); aerr != nil {
			return called{}, aerr
		}
	}
	return called{raw: raw.String(), reasoning: reasoning.String(), parser: p, toolCalls: toolCalls, result: res}, err
}

// spend adds what a call used to the run's usage: as the provider
// reported it, or, for a call that wrote something without a report (cut
// short), worked out from its messages and what it wrote. A call that
// failed before writing anything costs nothing.
func (r *analysisRun) spend(req Request, u Usage, wrote string, calledTools bool) {
	r.mu.Lock()
	switch {
	case u.reported():
		r.usage.Add(u)
		r.charged = true
	case wrote != "" || calledTools:
		in, out := int64(EstimateMessages(req.Messages)), int64(EstimateTokens(wrote))
		r.usage.Add(Usage{Input: &in, Output: &out})
		r.estimated, r.charged = true, true
	}
	total := r.usage
	r.mu.Unlock()
	if r.run.Spent != nil {
		r.run.Spent(total)
	}
}

// peek tells Spent what the run has used with the call that is writing,
// worked out from its messages and what it wrote so far.
func (r *analysisRun) peek(asked int64, wrote string) {
	if r.run.Spent == nil {
		return
	}
	out := int64(EstimateTokens(wrote))
	r.mu.Lock()
	total := r.usage
	r.mu.Unlock()
	total.Add(Usage{Input: &asked, Output: &out})
	r.run.Spent(total)
}

// addProvided notes sources of the provider's own search.
func (r *analysisRun) addProvided(sources []WebSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provided = append(r.provided, sources...)
}

// errIdle cancels a model call that went quiet.
var errIdle = errors.New("model call idle")

// stream makes the adapter's call, failing it as a connection failure when
// the model sends nothing for idleTimeout.
func (r *analysisRun) stream(ctx context.Context, req Request, emit func(Event) error) (Result, error) {
	callCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := time.AfterFunc(idleTimeout, func() { cancel(errIdle) })
	defer idle.Stop()
	res, err := r.run.Turn.Adapter.Stream(callCtx, req, func(e Event) error {
		idle.Reset(idleTimeout)
		return emit(e)
	})
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(callCtx), errIdle) {
		// a connection failure, not the run's deadline (#118)
		return res, &Error{Category: ErrUnavailable, Detail: fmt.Sprintf("no event for %s", idleTimeout)}
	}
	return res, err
}

// execute carries out a step's tool calls, at most MaxParallel of them and
// at once, and answers each in order.
func (r *analysisRun) execute(ctx context.Context, calls []ToolCall) []Message {
	out := make([]Message, len(calls))
	var wg sync.WaitGroup
	for i, c := range calls {
		out[i] = Message{Role: RoleTool, ToolCallID: c.ID}
		if i >= MaxParallel {
			out[i].Content = tooManyCalls
			continue
		}
		r.mu.Lock()
		r.toolRuns++
		id := fmt.Sprintf("tool-%d", r.toolRuns)
		r.mu.Unlock()
		wg.Go(func() { out[i].Content = r.runTool(ctx, id, c) })
	}
	wg.Wait()
	return out
}

// runTool answers one tool call. A failure never ends the run: it becomes
// what the model is told (#117).
func (r *analysisRun) runTool(ctx context.Context, id string, c ToolCall) string {
	t, ok := r.tools[c.Name]
	if !ok {
		names := make([]string, 0, len(r.run.Tools))
		for _, t := range r.run.Tools {
			names = append(names, t.Spec().Name)
		}
		return fmt.Sprintf(unknownFormat, c.Name, strings.Join(names, "、"))
	}
	b := r.breakers[c.Name]
	r.mu.Lock()
	off := b.off
	r.mu.Unlock()
	if off {
		return offNote(c.Name)
	}
	act := Activity{ID: id, Kind: c.Name, Target: safeTarget(t, c.Arguments), Status: ActivityRunning}
	_ = r.out.Activity(act)
	res, terr := r.attempt(ctx, t, c.Arguments)
	if terr != nil && terr.Failure == FailureTransient && ctx.Err() == nil {
		select {
		case <-time.After(toolBackoff):
			res, terr = r.attempt(ctx, t, c.Arguments)
		case <-ctx.Done():
		}
	}
	r.mu.Lock()
	switch {
	case terr == nil:
		b.failures = 0
	case terr.Failure == FailureAccount:
		b.off = true
	case terr.Failure == FailureTransient:
		b.failures++
		b.off = b.failures >= 2
	}
	r.mu.Unlock()
	if terr == nil {
		act.Status = ActivityDone
		_ = r.out.Activity(act)
		return res
	}
	act.Status, act.Reason = ActivityFailed, terr.Reason
	act.OwnKey = r.web.ownKeyFailure(c.Name)
	_ = r.out.Activity(act)
	switch terr.Failure {
	case FailureInvalid:
		return terr.Message
	case FailureTransient:
		return transientNote
	case FailureAccount:
		return offNote(c.Name)
	}
	logToolFailure(c.Name, terr)
	return internalNote
}

// attempt calls a tool once, within toolTimeout, turning a panic into an
// internal failure.
func (r *analysisRun) attempt(ctx context.Context, t Tool, args string) (res string, terr *ToolError) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			res, terr = "", &ToolError{Failure: FailureInternal, Reason: ReasonInternal, Cause: fmt.Errorf("panic: %v", p)}
		}
	}()
	res, err := t.Execute(ctx, args)
	if err != nil {
		return "", toolErrorOf(err)
	}
	return res, nil
}

// safeTarget is the call's target, or none when working it out panics.
func safeTarget(t Tool, args string) (target string) {
	defer func() {
		if p := recover(); p != nil {
			target = ""
		}
	}()
	return t.Target(args)
}

// offNote is what a call to a tool that is off for the run is answered:
// the capability by name, so the model answers without it.
func offNote(tool string) string {
	name, ok := capabilityNames[tool]
	if !ok {
		name = " " + tool + " "
	}
	return fmt.Sprintf(brokenFormat, name)
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
