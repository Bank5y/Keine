package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// NodeMermaid parses diagrams with mermaid's own parser, by running Script
// (eval/mermaid-check.mjs) with Node against the front end's node_modules
// in Web.
type NodeMermaid struct {
	Script, Web string
}

func (n NodeMermaid) Parse(diagrams []string) ([]error, error) {
	in, err := json.Marshal(diagrams)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", n.Script, n.Web)
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, firstLine(stderr.String()))
	}
	var msgs []*string
	if err := json.Unmarshal(raw, &msgs); err != nil || len(msgs) != len(diagrams) {
		return nil, fmt.Errorf("unreadable answer from %s: %q", n.Script, raw)
	}
	out := make([]error, len(msgs))
	for i, m := range msgs {
		if m != nil {
			out[i] = errors.New(*m)
		}
	}
	return out, nil
}
