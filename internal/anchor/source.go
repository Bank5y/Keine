package anchor

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// lookahead bounds how far into the source SourceSpan looks for the next
// visible character before deciding the reader shows a character the source
// does not hold (a footnote number, a back link).
const lookahead = 64

// sourceChar is a non-whitespace character of the source that may show in
// the reader, and where it starts in the source.
type sourceChar struct {
	r  rune
	at int
}

// SourceSpan maps a Range of a lesson's visible text (MarkdownText) back to
// the Markdown source: [start, end) are byte offsets of the source that hold
// the range, markup inside it included. It reports false when the ends of
// the range cannot be told apart in the source.
func SourceSpan(source string, r Range) (start, end int, ok bool) {
	if r.Start >= r.End {
		return 0, 0, false
	}
	pos := align(MarkdownText(source).chars, visibleSource(source))
	if r.End > len(pos) || pos[r.Start] < 0 || pos[r.End-1] < 0 {
		return 0, 0, false
	}
	_, size := utf8.DecodeRuneInString(source[pos[r.End-1]:])
	return pos[r.Start], pos[r.End-1] + size, true
}

// align matches each visible character, in order, to the next equal source
// character; -1 marks one the source does not hold nearby.
func align(visible []rune, src []sourceChar) []int {
	pos := make([]int, len(visible))
	j := 0
	for i, c := range visible {
		pos[i] = -1
		for k := j; k < len(src) && k < j+lookahead; k++ {
			if src[k].r == c {
				pos[i], j = src[k].at, k+1
				break
			}
		}
	}
	return pos
}

// visibleSource lists the source characters that render as text, in
// document order: text and code, but not link targets, image alt text or
// the tags of inline HTML.
func visibleSource(source string) []sourceChar {
	b := []byte(source)
	var out []sourceChar
	add := func(seg text.Segment) {
		for i, c := range source[seg.Start:seg.Stop] {
			if !isSpace(c) {
				out = append(out, sourceChar{r: c, at: seg.Start + i})
			}
		}
	}
	_ = ast.Walk(md.Parser().Parse(text.NewReader(b)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image, *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			add(n.Segment)
		case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				add(lines.At(i))
			}
		}
		return ast.WalkContinue, nil
	})
	return out
}

// HeadingsAt is the heading path of a place in the source (a byte offset):
// the nearest heading of each level above it, outermost first.
func HeadingsAt(source string, offset int) []string {
	b := []byte(source)
	type heading struct {
		level int
		title string
	}
	var stack []heading
	_ = ast.Walk(md.Parser().Parse(text.NewReader(b)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, isHeading := n.(*ast.Heading)
		if !entering || !isHeading {
			return ast.WalkContinue, nil
		}
		if h.Lines().Len() == 0 || h.Lines().At(0).Start >= offset {
			return ast.WalkStop, nil
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, heading{level: h.Level, title: inlineText(h, source)})
		return ast.WalkSkipChildren, nil
	})
	out := make([]string, len(stack))
	for i, h := range stack {
		out[i] = h.title
	}
	return out
}

// inlineText is the plain text of a heading's inline content.
func inlineText(n ast.Node, source string) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := c.(*ast.Text); ok && entering {
			sb.WriteString(source[t.Segment.Start:t.Segment.Stop])
			if t.SoftLineBreak() || t.HardLineBreak() {
				sb.WriteByte(' ')
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(sb.String())
}
