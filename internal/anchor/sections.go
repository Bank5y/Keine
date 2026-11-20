package anchor

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Section is the part of a lesson under one heading, up to the next heading
// of the same or a higher level.
type Section struct {
	// ID is the heading's anchor in the reader (rehype-slug), stable as long
	// as the heading's words are.
	ID    string
	Title string
	Level int
	// Start and End are where it sits in the source, in bytes, its heading
	// included.
	Start, End int
}

// Sections are a lesson's sections in source order, nested ones included.
func Sections(source string) []Section {
	b := []byte(source)
	var out []Section
	slugs := slugger{}
	_ = ast.Walk(md.Parser().Parse(text.NewReader(b)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		if h.Lines().Len() == 0 {
			return ast.WalkSkipChildren, nil
		}
		title := inlineText(h, source)
		out = append(out, Section{ID: slugs.slug(title), Title: title, Level: h.Level, Start: lineStart(source, h.Lines().At(0).Start)})
		return ast.WalkSkipChildren, nil
	})
	for i := range out {
		out[i].End = len(source)
		for _, next := range out[i+1:] {
			if next.Level <= out[i].Level {
				out[i].End = next.Start
				break
			}
		}
	}
	return out
}

// lineStart is the start of the line holding offset, which is where a
// heading's "#" marks are.
func lineStart(source string, offset int) int {
	return strings.LastIndexByte(source[:offset], '\n') + 1
}

// slugger makes heading anchors the way the reader does (github-slugger):
// lower case, only letters, marks, numbers, "_", "-" and spaces kept, spaces
// turned into "-", and a repeated anchor numbered.
type slugger map[string]int

func (s slugger) slug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || r == '_' || r == '-':
			b.WriteRune(r)
		}
	}
	base := b.String()
	out := base
	for {
		if _, taken := s[out]; !taken {
			break
		}
		s[base]++
		out = base + "-" + strconv.Itoa(s[base])
	}
	s[out] = 0
	return out
}
