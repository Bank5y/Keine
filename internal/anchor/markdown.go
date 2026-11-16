package anchor

import (
	"bytes"
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	xhtml "golang.org/x/net/html"
)

// md renders course Markdown the way the reader does (CommonMark, GFM and
// raw HTML), so its text matches what the reader shows.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Footnote),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// hidden elements show no text in the reader: the sanitizer drops them.
var hidden = map[string]bool{"script": true, "style": true, "template": true, "noscript": true, "iframe": true, "object": true, "svg": true, "math": true}

// MarkdownText is the visible text of a lesson's Markdown: what a reader
// can select, without markup, images or the alt text of images.
func MarkdownText(source string) Text {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return NewText(source)
	}
	return NewText(htmlText(&buf))
}

func htmlText(r io.Reader) string {
	var sb strings.Builder
	z := xhtml.NewTokenizer(r)
	skip := 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return sb.String()
		case xhtml.TextToken:
			if skip == 0 {
				sb.Write(z.Text())
			}
		case xhtml.StartTagToken:
			if name, _ := z.TagName(); hidden[string(name)] {
				skip++
			}
		case xhtml.EndTagToken:
			if name, _ := z.TagName(); hidden[string(name)] && skip > 0 {
				skip--
			}
		}
	}
}
