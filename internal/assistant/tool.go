package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
)

// Tool is a tool the assistant may call (ADR 0008): a declaration whose
// parameters come from a Go struct, and what calling it does. A tool
// reaches data only through what it was built with for the run (the
// reader, the course), so what it may read is Keine's to decide.
type Tool interface {
	Spec() ToolSpec
	// Target is what the call is about as the reader sees it in progress
	// (a lesson's title, a keyword); "" when the arguments do not say.
	Target(args string) string
	// Execute answers a call. A failure is a *ToolError or, for anything
	// else, an internal error the model only hears about in general terms.
	Execute(ctx context.Context, args string) (string, error)
}

// ToolFailure is which of the four kinds of failure a tool met (#117).
type ToolFailure int

const (
	// FailureInvalid: the call was written wrong; the model is told how
	// to fix it and nothing is retried.
	FailureInvalid ToolFailure = iota
	// FailureTransient: a timeout, a rate limit, a server or network error;
	// the call is tried once more.
	FailureTransient
	// FailureAccount: the service refuses the account (no balance, bad
	// key); the tool is off for the rest of the run.
	FailureAccount
	// FailureInternal: a panic, a database error; the model hears only that
	// the call got nothing.
	FailureInternal
)

// Reasons of a failed activity besides the provider categories.
const (
	ReasonUnsupportedContent Category = "unsupported_content"
	ReasonInternal           Category = "internal"
)

// ToolError is a tool failure the model and the reader may hear about.
// Message is what the model is told; Cause is for the server log.
type ToolError struct {
	Failure ToolFailure
	Reason  Category
	Message string
	Cause   error
}

func (e *ToolError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("tool: %s: %s: %v", e.Reason, e.Message, e.Cause)
	}
	return fmt.Sprintf("tool: %s: %s", e.Reason, e.Message)
}

// Invalid is a call written wrong: message says how to write it instead.
func Invalid(reason Category, message string) error {
	return &ToolError{Failure: FailureInvalid, Reason: reason, Message: message}
}

// Transient is a failure worth one more try.
func Transient(reason Category, cause error) error {
	return &ToolError{Failure: FailureTransient, Reason: reason, Cause: cause}
}

// AccountFailure is the service refusing the account.
func AccountFailure(reason Category, cause error) error {
	return &ToolError{Failure: FailureAccount, Reason: reason, Cause: cause}
}

// NewTool makes a tool whose arguments are the JSON form of A. A's exported
// fields are its parameters: named by their json tag, described by their
// desc tag, required unless the json tag says omitempty. Strings, integers
// and booleans are the only kinds of field.
func NewTool[A any](name, description string, target func(A) string, exec func(context.Context, A) (string, error)) Tool {
	return &typedTool[A]{
		spec:   ToolSpec{Name: name, Description: description, Parameters: schemaOf(reflect.TypeFor[A]())},
		target: target,
		exec:   exec,
	}
}

type typedTool[A any] struct {
	spec   ToolSpec
	target func(A) string
	exec   func(context.Context, A) (string, error)
}

func (t *typedTool[A]) Spec() ToolSpec { return t.spec }

func (t *typedTool[A]) Target(args string) string {
	a, err := t.parse(args)
	if err != nil {
		return ""
	}
	return t.target(a)
}

func (t *typedTool[A]) Execute(ctx context.Context, args string) (string, error) {
	a, err := t.parse(args)
	if err != nil {
		return "", err
	}
	return t.exec(ctx, a)
}

// parse reads the arguments strictly: unknown, missing or mistyped
// parameters are a call written wrong, answered with the schema.
func (t *typedTool[A]) parse(args string) (A, error) {
	var a A
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	d := json.NewDecoder(strings.NewReader(args))
	d.DisallowUnknownFields()
	fix := func(what string) error {
		return Invalid(ErrInvalidRequest, fmt.Sprintf("参数%s。参数应符合：%s", what, t.spec.Parameters))
	}
	if err := d.Decode(&a); err != nil {
		return a, fix("不是合法的 JSON 或类型不对")
	}
	var present map[string]json.RawMessage
	_ = json.Unmarshal([]byte(args), &present)
	for _, f := range fields(reflect.TypeFor[A]()) {
		if v, ok := present[f.name]; f.required && (!ok || bytes.Equal(v, []byte("null"))) {
			return a, fix("缺少 " + f.name)
		}
	}
	return a, nil
}

type field struct {
	name, kind, desc string
	required         bool
}

func fields(t reflect.Type) []field {
	var out []field
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")
		name := tag[0]
		if name == "" {
			name = f.Name
		}
		var kind string
		switch f.Type.Kind() {
		case reflect.String:
			kind = "string"
		case reflect.Int, reflect.Int64, reflect.Int32:
			kind = "integer"
		case reflect.Bool:
			kind = "boolean"
		default:
			panic("tool parameter " + name + " is of an unsupported kind")
		}
		out = append(out, field{name: name, kind: kind, desc: f.Tag.Get("desc"), required: !strings.Contains(f.Tag.Get("json"), "omitempty")})
	}
	return out
}

func schemaOf(t reflect.Type) json.RawMessage {
	props := map[string]any{}
	required := []string{}
	for _, f := range fields(t) {
		p := map[string]any{"type": f.kind}
		if f.desc != "" {
			p["description"] = f.desc
		}
		props[f.name] = p
		if f.required {
			required = append(required, f.name)
		}
	}
	b, err := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false})
	if err != nil {
		panic(err)
	}
	return b
}

// toolErrorOf classifies what a tool returned or how it ended: a context
// deadline is a timeout worth one more try, anything not a *ToolError is
// internal.
func toolErrorOf(err error) *ToolError {
	var te *ToolError
	if errors.As(err, &te) {
		return te
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ToolError{Failure: FailureTransient, Reason: ErrTimeout, Cause: err}
	}
	return &ToolError{Failure: FailureInternal, Reason: ReasonInternal, Cause: err}
}

// logToolFailure keeps the cause of an internal failure, which the model
// and the reader never see.
func logToolFailure(tool string, e *ToolError) {
	slog.Error("assistant tool failed", "tool", tool, "reason", e.Reason, "err", e.Cause)
}
