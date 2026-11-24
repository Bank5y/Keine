package assistant

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Citations number the web sources of one exploration dialogue (ADR 0013):
// the whole tree of nodes shares them, and an address has one number for
// good. The model sees the numbers on its search results and pages, and
// cites a source by writing [n]; the system turns that into the link it
// recorded. A number freed by a deleted node may be given again, since
// every reply that cited it went with that node.
type Citations struct {
	mu    sync.Mutex
	byURL map[string]int
	next  int
}

// NewCitations goes on numbering from the sources the dialogue already
// numbered (those with N set).
func NewCitations(known []WebSource) *Citations {
	c := &Citations{byURL: map[string]int{}, next: 1}
	for _, s := range known {
		if s.N <= 0 {
			continue
		}
		if _, ok := c.byURL[s.URL]; !ok {
			c.byURL[s.URL] = s.N
		}
		c.next = max(c.next, s.N+1)
	}
	return c
}

// number is the address's number, a new one the first time.
func (c *Citations) number(url string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.byURL[url]; ok {
		return n
	}
	n := c.next
	c.byURL[url] = n
	c.next++
	return n
}

// citableHead heads the sources a turn may cite, in its question.
const citableHead = "【可引用的资料】回复里用 [编号] 引用："

// citable lists the numbered sources by number, each once, with its title:
// what a turn may cite of the turns before it. "" when there are none.
func citable(sources []WebSource) string {
	byN := map[int]string{}
	for _, s := range sources {
		if s.N > 0 && byN[s.N] == "" {
			byN[s.N] = cmpOr(strings.TrimSpace(s.Title), HostOf(s.URL))
		}
	}
	if len(byN) == 0 {
		return ""
	}
	ns := make([]int, 0, len(byN))
	for n := range byN {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	lines := []string{citableHead}
	for _, n := range ns {
		lines = append(lines, fmt.Sprintf("[%d] %s", n, byN[n]))
	}
	return strings.Join(lines, "\n")
}

func cmpOr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// citation is a citation as a reply writes it: one number in brackets, or
// several, comma separated.
var citation = regexp.MustCompile(`\[(\d+(?:\s*[,，]\s*\d+)*)\]`)

// codeSpan is a fenced block or an inline code span, where brackets are
// code and cite nothing.
var codeSpan = regexp.MustCompile("(?s)(```|~~~).*?(```|~~~)|`[^`\n]*`")

// Cited are the numbers a reply cites, in the order written, each once.
func Cited(md string) []int {
	var out []int
	for _, m := range citation.FindAllStringSubmatch(codeSpan.ReplaceAllString(md, ""), -1) {
		for _, f := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == '，' || r == ' ' }) {
			if n, err := strconv.Atoi(f); err == nil && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}
