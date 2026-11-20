package anchor_test

import (
	"testing"

	"github.com/Bank5y/keine/internal/anchor"
)

// spanOf locates quote q in the rendered text of source and returns the
// part of the source that SourceSpan maps it back to.
func spanOf(t *testing.T, source string, q anchor.Quote) string {
	t.Helper()
	r, ok := anchor.MarkdownText(source).Locate(q)
	if !ok {
		t.Fatalf("quote %q not in the text", q.Exact)
	}
	start, end, ok := anchor.SourceSpan(source, r)
	if !ok {
		t.Fatalf("no source span for %+v", r)
	}
	return source[start:end]
}

func TestSourceSpanOfPlainText(t *testing.T) {
	src := "# 标题\n\nAgent 不是一个新概念，但在大模型出现之后，它第一次变得真正可用。\n"
	if got := spanOf(t, src, anchor.Quote{Exact: "第一次变得真正可用"}); got != "第一次变得真正可用" {
		t.Fatalf("got %q", got)
	}
}

func TestSourceSpanPicksTheSelectedOccurrence(t *testing.T) {
	src := "Agent 在开头。\n\n## 小节\n\n这里也有 Agent，后面还有。\n"
	r, _ := anchor.MarkdownText(src).Locate(anchor.Quote{Exact: "Agent", Prefix: "这里也有"})
	start, end, ok := anchor.SourceSpan(src, r)
	if !ok || src[start:end] != "Agent" || start < len("Agent 在开头。\n\n## 小节\n\n") {
		t.Fatalf("got [%d,%d) %q ok=%v", start, end, src[start:end], ok)
	}
}

func TestSourceSpanCoversMarkupInsideTheSelection(t *testing.T) {
	src := "把所有**逻辑塞进**一个提示词里。\n"
	if got := spanOf(t, src, anchor.Quote{Exact: "所有逻辑塞进一个"}); got != "所有**逻辑塞进**一个" {
		t.Fatalf("got %q", got)
	}
}

func TestSourceSpanAcrossALineBreak(t *testing.T) {
	src := "很多团队在第一次落地时\n都会遇到同一个坑。\n"
	if got := spanOf(t, src, anchor.Quote{Exact: "落地时都会遇到"}); got != "落地时\n都会遇到" {
		t.Fatalf("got %q", got)
	}
}

func TestSourceSpanSkipsImageAltText(t *testing.T) {
	src := "![执行流程](images/flow.png)\n\n下面是执行流程的说明。\n"
	if got := spanOf(t, src, anchor.Quote{Exact: "执行流程的说明"}); got != "执行流程的说明" {
		t.Fatalf("got %q", got)
	}
}

func TestHeadingsAtTheSelection(t *testing.T) {
	src := "# 01｜用户输入规范化\n\n开头。\n\n## 一个最小的例子\n\n### 细节\n\n正文在这里。\n\n## 小结\n\n结尾。\n"
	r, _ := anchor.MarkdownText(src).Locate(anchor.Quote{Exact: "正文在这里"})
	start, _, _ := anchor.SourceSpan(src, r)
	got := anchor.HeadingsAt(src, start)
	want := []string{"01｜用户输入规范化", "一个最小的例子", "细节"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	r, _ = anchor.MarkdownText(src).Locate(anchor.Quote{Exact: "结尾"})
	start, _, _ = anchor.SourceSpan(src, r)
	if got := anchor.HeadingsAt(src, start); len(got) != 2 || got[1] != "小结" {
		t.Fatalf("a sibling section replaces the deeper one, got %q", got)
	}
}
