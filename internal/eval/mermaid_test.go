package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestNodeParsesWithMermaid runs the real parser when the front end's
// dependencies are installed (they are not in CI's Go job).
func TestNodeParsesWithMermaid(t *testing.T) {
	root, _ := filepath.Abs("../..")
	web := filepath.Join(root, "web")
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	if _, err := os.Stat(filepath.Join(web, "node_modules", "mermaid")); err != nil {
		t.Skip("web/node_modules not installed")
	}
	n := NodeMermaid{Script: filepath.Join(root, "eval", "mermaid-check.mjs"), Web: web}
	errs, err := n.Parse([]string{"flowchart TD\n  A --> B", "flowchart TD\n  A -->", "stateDiagram-v2\n  [*] --> S\n  S --> [*]", "sequenceDiagram\n  A->>B: hi"})
	if err != nil {
		t.Fatal(err)
	}
	if errs[0] != nil || errs[1] == nil || errs[2] != nil || errs[3] != nil {
		t.Fatalf("parse results: %v", errs)
	}
}
