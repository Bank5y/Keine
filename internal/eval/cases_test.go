package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/course"
)

func TestAFollowupCaseDescribesTheTurnsAfterThePinnedAnalysis(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "cases.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`category: 系统与网络
cases:
  - id: tcp-small-packets
    kind: child
    course: 3
    version: v1
    lesson: l01
    quote: TCP
    turns:
      - ask: TCP 怎么保证可靠？
      - fork: child
        ask: 小数据包怎么传？
        must_include: [Nagle]
        must_not_link: [4C]
      - ask: 延迟确认会和它冲突吗？
`)
	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatal(err)
	}
	turns := cases[0].Turns
	if len(turns) != 3 || turns[0].Fork != "" || turns[1].Fork != course.NodeChild || turns[1].MustInclude[0] != "Nagle" || turns[1].MustNotLink[0] != "4C" || turns[2].Ask != "延迟确认会和它冲突吗？" {
		t.Fatalf("turns: %+v", turns)
	}

	write("cases:\n  - {id: x, course: 3, version: v1, lesson: l01, quote: TCP, turns: [{fork: main, ask: 再问}]}\n")
	if _, err := LoadCases(dir); err == nil || !strings.Contains(err.Error(), "fork must be") {
		t.Fatalf("a main node opened: %v", err)
	}
	write("cases:\n  - {id: x, course: 3, version: v1, lesson: l01, quote: TCP, turns: [{ask: 再问, must_not_link: [4C]}]}\n")
	if _, err := LoadCases(dir); err == nil || !strings.Contains(err.Error(), "must_not_link") {
		t.Fatalf("a turn that links nothing names what it must not link: %v", err)
	}
	write("cases:\n  - {id: x, course: 3, version: v1, lesson: l01, quote: TCP, turns: [{fork: child}]}\n")
	if _, err := LoadCases(dir); err == nil || !strings.Contains(err.Error(), "missing ask") {
		t.Fatalf("a turn without a question: %v", err)
	}
}
