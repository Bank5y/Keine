package course

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Find is the entries whose name or an alias is term, compared
// normalized (NormalizeConcept).
func (x ConceptIndex) Find(term string) []ConceptEntry {
	want := NormalizeConcept(term)
	var out []ConceptEntry
	for _, e := range x.Entries {
		for _, n := range append([]string{e.Name}, e.Aliases...) {
			if NormalizeConcept(n) == want {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// minMentionRunes is the shortest a name or an alias can be, normalized,
// and still be looked for in a text: a single character matches anywhere.
const minMentionRunes = 2

// Mention is an entry a text names, and where it first names it: in
// characters of the text normalized (NormalizeConcept).
type Mention struct {
	Entry ConceptEntry
	At    int
}

// Mentioned is the entries a text names, by name or alias, compared
// normalized (NormalizeConcept), in the order the text first names them
// (ADR 0011). Entries of the lesson keyed exceptLesson are left out, but
// still hide the shorter names they overlap.
//
// A name's letters and digits only match a whole word: "4C" is not named
// in "G4C" (#177). Where two entries' names overlap in the text, only the
// longer name is named there: "日志复制" names no "日志"; of names as long
// as each other, both are.
func (x ConceptIndex) Mentioned(text, exceptLesson string) []Mention {
	t, breaks := normalizeWithBreaks(text)
	type span struct{ entry, start, end, runes int }
	var spans []span
	for ei, e := range x.Entries {
		for _, n := range append([]string{e.Name}, e.Aliases...) {
			n = NormalizeConcept(n)
			runes := utf8.RuneCountInString(n)
			if runes < minMentionRunes {
				continue
			}
			for from := 0; from < len(t); {
				i := strings.Index(t[from:], n)
				if i < 0 {
					break
				}
				i += from
				if j := i + len(n); wordStart(t, breaks, i, n) && wordEnd(t, breaks, j, n) {
					spans = append(spans, span{ei, i, j, runes})
				}
				_, size := utf8.DecodeRuneInString(t[i:])
				from = i + size
			}
		}
	}
	first := map[int]int{}
	for _, s := range spans {
		shadowed := slices.ContainsFunc(spans, func(o span) bool {
			return o.entry != s.entry && o.runes > s.runes && o.start < s.end && s.start < o.end
		})
		if x.Entries[s.entry].Lesson.Key == exceptLesson {
			continue
		}
		if at, ok := first[s.entry]; !shadowed && (!ok || s.start < at) {
			first[s.entry] = s.start
		}
	}
	var named []int
	for ei := range first {
		named = append(named, ei)
	}
	slices.SortFunc(named, func(a, b int) int {
		if first[a] != first[b] {
			return first[a] - first[b]
		}
		return a - b
	})
	out := make([]Mention, len(named))
	for i, ei := range named {
		out[i] = Mention{Entry: x.Entries[ei], At: utf8.RuneCountInString(t[:first[ei]])}
	}
	return out
}

// normalizeWithBreaks is NormalizeConcept(s), and the places in it where
// white space was taken out: a word ends there.
func normalizeWithBreaks(s string) (string, map[int]bool) {
	var b strings.Builder
	breaks := map[int]bool{}
	for i, f := range strings.Fields(strings.ToLower(norm.NFKC.String(s))) {
		if i > 0 {
			breaks[b.Len()] = true
		}
		b.WriteString(f)
	}
	return b.String(), breaks
}

// wordRune is a letter or digit of a word that white space or other
// characters end: not one of the scripts written without spaces.
func wordRune(r rune) bool {
	if unicode.IsDigit(r) {
		return true
	}
	return unicode.IsLetter(r) && !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// wordStart is whether name n found at i in t starts a word there, when it
// starts with a letter or digit.
func wordStart(t string, breaks map[int]bool, i int, n string) bool {
	if r, _ := utf8.DecodeRuneInString(n); !wordRune(r) || i == 0 || breaks[i] {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(t[:i])
	return !wordRune(r)
}

// wordEnd is whether name n found ending at j in t ends a word there, when
// it ends with a letter or digit.
func wordEnd(t string, breaks map[int]bool, j int, n string) bool {
	if r, _ := utf8.DecodeLastRuneInString(n); !wordRune(r) || j == len(t) || breaks[j] {
		return true
	}
	r, _ := utf8.DecodeRuneInString(t[j:])
	return !wordRune(r)
}

// NormalizeConcept is how concept names compare: width and compatibility
// forms folded (NFKC), lower case, white space taken out.
func NormalizeConcept(s string) string {
	t, _ := normalizeWithBreaks(s)
	return t
}
