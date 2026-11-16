package anchor

import (
	"math"
	"unicode/utf8"
)

// Suggestion is where the magic wand of annotation migration proposes to put
// a quote in a text that changed: the place, the quote as a selection of
// exactly that place would keep it, and how sure it is, from 0 (not at all)
// to 1 (the quoted text and its surroundings are all there).
type Suggestion struct {
	Range      Range
	Quote      Quote
	Confidence float64
}

const (
	// contextLen is how many visible characters of surroundings a quote
	// keeps on each side, as the reader takes them
	contextLen = 32
	// minSimilar is how alike the quoted text and a place's text must be at
	// least; below it the quote counts as gone
	minSimilar = 0.4
	// minConfidence is the least confidence worth suggesting
	minConfidence = 0.5
	// a place whose runner-up elsewhere is within ambiguous of it is not one
	// to be sure of
	ambiguous      = 0.03
	ambiguousShare = 0.8
)

// how much the quoted text, its prefix and suffix, and where it used to sit
// count towards a place's score
const (
	weightQuote    = 0.5
	weightContext  = 0.2
	weightPosition = 0.1
)

// Suggest proposes a place for a quote that Relocate could not put back with
// confidence. It is deterministic, without AI (ADR-0003): it looks for
// stretches of text within a few edits of the quoted text (Bitap's question,
// answered by Sellers' edit-distance search so long quotes are no problem)
// and for the stretch between where the prefix ends and the suffix starts
// (the quoted text was reworded), then scores each place by how alike its
// text, its surroundings and its position are to the quote's. was is the text
// the quote was taken from, for its old position; a zero Text when unknown.
// It reports false when no place is likely enough, say the quoted text was
// deleted.
func (t Text) Suggest(q Quote, was Text) (Suggestion, bool) {
	exact := visible(q.Exact)
	if len(exact) == 0 || len(t.chars) == 0 {
		return Suggestion{}, false
	}
	prefix, suffix := visible(q.Prefix), visible(q.Suffix)
	if len(prefix) > contextLen {
		prefix = prefix[len(prefix)-contextLen:]
	}
	if len(suffix) > contextLen {
		suffix = suffix[:contextLen]
	}
	near := -1.0
	if r, ok := was.Locate(q); ok {
		near = float64(r.Start) / float64(len(was.chars))
	}

	type place struct {
		r     Range
		score float64
	}
	var places []place
	seen := map[Range]bool{}
	consider := func(r Range, dist int) {
		if seen[r] {
			return
		}
		seen[r] = true
		similar := 1 - float64(dist)/float64(max(len(exact), r.End-r.Start))
		if similar < minSimilar {
			return
		}
		score, weight := weightQuote*similar, weightQuote
		if len(prefix) > 0 {
			score += weightContext * likeness(t.chars[max(0, r.Start-len(prefix)):r.Start], prefix)
			weight += weightContext
		}
		if len(suffix) > 0 {
			score += weightContext * likeness(t.chars[r.End:min(len(t.chars), r.End+len(suffix))], suffix)
			weight += weightContext
		}
		if near >= 0 {
			score += weightPosition * (1 - math.Abs(float64(r.Start)/float64(len(t.chars))-near))
			weight += weightPosition
		}
		places = append(places, place{r, score / weight})
	}

	for _, m := range approx(t.chars, exact, len(exact)/2) {
		consider(m.Range, m.dist)
	}
	if len(prefix) > 0 && len(suffix) > 0 {
		ends, starts := approx(t.chars, prefix, len(prefix)/4), approx(t.chars, suffix, len(suffix)/4)
		for _, p := range ends {
			for _, s := range starts {
				if r := (Range{Start: p.End, End: s.Start}); r.Start < r.End && r.End-r.Start <= 2*len(exact)+16 && !seen[r] {
					consider(r, distance(t.chars[r.Start:r.End], exact))
				}
			}
		}
	}

	best := -1
	for i, p := range places {
		if best < 0 || better(p.score, p.r, places[best].score, places[best].r) {
			best = i
		}
	}
	if best < 0 {
		return Suggestion{}, false
	}
	b := places[best]
	confidence := b.score
	for _, p := range places {
		if (p.r.End <= b.r.Start || p.r.Start >= b.r.End) && p.score > b.score-ambiguous {
			confidence *= ambiguousShare
			break
		}
	}
	confidence = math.Round(confidence*100) / 100
	if confidence < minConfidence {
		return Suggestion{}, false
	}
	return Suggestion{Range: b.r, Quote: t.Quote(b.r), Confidence: confidence}, true
}

// better orders places: higher score first, then the earlier, then the
// shorter, so the same input always gives the same suggestion.
func better(score float64, r Range, than float64, tr Range) bool {
	if score != than {
		return score > than
	}
	if r.Start != tr.Start {
		return r.Start < tr.Start
	}
	return r.End < tr.End
}

// likeness is how alike a stretch of text is to a pattern, 0 to 1.
func likeness(text, pattern []rune) float64 {
	return max(0, 1-float64(distance(text, pattern))/float64(len(pattern)))
}

type match struct {
	Range
	dist int
}

// approx finds, for every place in text where a stretch within k edits of
// pattern ends, that stretch and its edit distance (Sellers' algorithm:
// edit distance where the pattern may start anywhere in the text).
func approx(text, pattern []rune, k int) []match {
	m := len(pattern)
	prev, cur := make([]int, m+1), make([]int, m+1)
	pstart, cstart := make([]int, m+1), make([]int, m+1)
	for i := range prev {
		prev[i] = i
	}
	var out []match
	for j := 1; j <= len(text); j++ {
		cur[0], cstart[0] = 0, j
		for i := 1; i <= m; i++ {
			cost := 1
			if pattern[i-1] == text[j-1] {
				cost = 0
			}
			d, st := prev[i-1]+cost, pstart[i-1]
			if v := cur[i-1] + 1; v < d {
				d, st = v, cstart[i-1]
			}
			if v := prev[i] + 1; v < d {
				d, st = v, pstart[i]
			}
			cur[i], cstart[i] = d, st
		}
		if cur[m] <= k && cstart[m] < j {
			out = append(out, match{Range{Start: cstart[m], End: j}, cur[m]})
		}
		prev, cur = cur, prev
		pstart, cstart = cstart, pstart
	}
	return out
}

// distance is the edit distance between a and b.
func distance(a, b []rune) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j-1]+cost, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// Quote reads a range of the text as a selection of exactly that text keeps
// it: the text with its spacing, and up to contextLen visible characters
// before and after it.
func (t Text) Quote(r Range) Quote {
	if r.Start < 0 || r.End > len(t.chars) || r.Start >= r.End {
		return Quote{}
	}
	from := t.at[r.Start]
	to := t.at[r.End-1] + utf8.RuneLen(t.chars[r.End-1])
	q := Quote{Exact: t.raw[from:to]}
	if r.Start > 0 {
		q.Prefix = t.raw[t.at[max(0, r.Start-contextLen)]:from]
	}
	if r.End < len(t.chars) {
		last := min(len(t.chars), r.End+contextLen) - 1
		q.Suffix = t.raw[to : t.at[last]+utf8.RuneLen(t.chars[last])]
	}
	return q
}
