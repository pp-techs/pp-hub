// Package textsearch holds the pure half of content search (M27): how text
// extracted from a file is normalised for the index, how a user's query is
// turned into the full-text and substring predicates, and how a result
// snippet is cut and highlighted.
//
// Everything that matches runs on *folded* text — lower-cased with
// diacritics removed — so "tieng viet", "Tiếng Việt" and "TIẾNG VIỆT" are
// the same query. Folding happens here rather than in Postgres: the unaccent
// extension is not guaranteed to be installed, and a folded ASCII word is
// split identically by the `simple` text-search parser under any database
// locale. Vietnamese needs no stemming, which `simple` does not do.
//
// Fold is rune-for-rune, so a position in folded text is the same position in
// the original. That is what lets the database locate a hit in
// node_texts.folded and hand back the matching window of node_texts.content.
package textsearch

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	// MaxIndexedBytes caps the text stored per file. A tsvector must stay
	// under 1 MB and each hit is re-scanned for its snippet; 256 KiB is far
	// more prose than any document a search result will be judged by.
	MaxIndexedBytes = 256 << 10

	// maxTerms bounds the full-text query: a pasted paragraph would otherwise
	// become a conjunction of hundreds of prefix scans.
	maxTerms = 8

	// minLikeRunes is the shortest phrase the substring fallback runs for.
	// Below a trigram the index cannot narrow LIKE '%…%' at all, and every
	// document in the drive would be read.
	minLikeRunes = 3

	// SnippetRunes is the length a snippet aims for, ellipses excluded.
	SnippetRunes = 160
	// snippetLead is how much text before the first hit a snippet keeps, so
	// the hit reads in context instead of opening the line.
	snippetLead = 40

	ellipsis = "…"
)

// Prepare turns extracted text into the two columns node_texts stores: the
// original (valid UTF-8, no NULs, NFC, capped at MaxIndexedBytes on a rune
// boundary) and its fold.
func Prepare(text string) (content, folded string) {
	text = strings.ToValidUTF8(text, "")
	text = strings.ReplaceAll(text, "\x00", "")
	text = norm.NFC.String(text)
	if len(text) > MaxIndexedBytes {
		cut := MaxIndexedBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return text, Fold(text)
}

// Fold lower-cases s and strips diacritics, one rune for one rune: the result
// has exactly as many runes as s. Expects NFC input (Prepare and ParseQuery
// normalise first); a combining mark that NFC could not compose is kept.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

func foldRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = foldRune(r)
	}
	return out
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		return unicode.ToLower(r)
	}
	switch r {
	// The Vietnamese letter đ is a distinct letter, not d with a mark, so
	// canonical decomposition leaves it alone — yet nobody types it when
	// searching without diacritics.
	case 'đ', 'Đ':
		return 'd'
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	if dec := norm.NFD.Properties(buf[:n]).Decomposition(); len(dec) > 0 {
		base, size := utf8.DecodeRune(dec)
		if onlyMarks(dec[size:]) {
			return unicode.ToLower(base)
		}
	}
	return unicode.ToLower(r)
}

func onlyMarks(b []byte) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if !unicode.Is(unicode.Mn, r) {
			return false
		}
		b = b[size:]
	}
	return true
}

// isWordRune matches what a search term is made of. Marks count so that a
// script whose vowels are combining marks (Thai, Devanagari) keeps its words
// whole.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// Query is a user's search string in the forms the index needs.
type Query struct {
	// Terms are the folded words of the query, deduplicated, in order, at
	// most maxTerms. Each is letters, digits and marks only, so it can be
	// quoted into a tsquery or a regular expression as-is.
	Terms []string
	// Phrase is the whole folded query with whitespace collapsed — what the
	// substring fallback looks for.
	Phrase string
}

// ParseQuery folds and tokenises a raw query string.
func ParseQuery(raw string) Query {
	folded := Fold(norm.NFC.String(raw))
	q := Query{Phrase: collapseSpace(folded)}
	for _, t := range strings.FieldsFunc(folded, func(r rune) bool { return !isWordRune(r) }) {
		if len(q.Terms) == maxTerms {
			break
		}
		if !slices.Contains(q.Terms, t) {
			q.Terms = append(q.Terms, t)
		}
	}
	return q
}

// TSQuery is the query for to_tsquery('simple', …): every term as a prefix,
// all required. Prefixes because the palette searches as the user types, so
// the last word is almost always incomplete. Empty when the query has no
// words (only punctuation), which the caller sends as NULL.
func (q Query) TSQuery() string {
	parts := make([]string, len(q.Terms))
	for i, t := range q.Terms {
		parts[i] = "'" + t + "':*"
	}
	return strings.Join(parts, " & ")
}

// LikePattern is the escaped substring for `folded LIKE '%' || … || '%'`,
// the fallback that finds what word-prefix matching cannot: a hit inside a
// word, punctuation such as "v2.1", text the parser does not split. Empty
// below minLikeRunes, where no index can serve it.
func (q Query) LikePattern() string {
	if utf8.RuneCountInString(q.Phrase) < minLikeRunes {
		return ""
	}
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q.Phrase)
}

// Snippet cuts about SnippetRunes of plain text around the first hit in
// excerpt and returns the highlight ranges within it.
//
// excerpt is a window of the stored content (see NodeTextExcerpts);
// moreBefore and moreAfter say whether the document continues past either
// edge. Whitespace is collapsed. A leading and/or trailing "…" is part of the
// returned snippet whenever text was cut on that side, and the highlights
// account for it: each is [start, end) in runes of the returned string,
// sorted and non-overlapping.
//
// A hit is a term at the start of a word, as the full-text query matches;
// when no term matches that way, every occurrence of the phrase, as the
// substring fallback matches.
func (q Query) Snippet(excerpt string, moreBefore, moreAfter bool) (string, [][2]int) {
	runes := []rune(collapseSpace(norm.NFC.String(excerpt)))
	folded := foldRunes(runes)
	hits := q.hits(folded)

	anchor, anchorEnd := 0, 0
	if len(hits) > 0 {
		anchor, anchorEnd = hits[0][0], hits[0][1]
	}
	start := max(anchor-snippetLead, 0)
	end := min(start+SnippetRunes, len(runes))
	if end == len(runes) {
		start = max(end-SnippetRunes, 0)
	}

	cutBefore := start > 0 || moreBefore
	cutAfter := end < len(runes) || moreAfter
	// Open and close on word boundaries, never at the expense of the hit.
	if cutBefore {
		if i := slices.Index(runes[start:anchor], ' '); i >= 0 {
			start += i + 1
		}
	}
	if cutAfter && end > anchorEnd {
		if i := lastIndex(runes[anchorEnd:end], ' '); i >= 0 {
			end = anchorEnd + i
		}
	}
	for start < end && runes[start] == ' ' {
		start++
	}
	for end > start && runes[end-1] == ' ' {
		end--
	}

	var b strings.Builder
	offset := -start
	if cutBefore {
		b.WriteString(ellipsis)
		offset++
	}
	b.WriteString(string(runes[start:end]))
	if cutAfter {
		b.WriteString(ellipsis)
	}

	var out [][2]int
	for _, h := range hits {
		s, e := max(h[0], start), min(h[1], end)
		if s < e {
			out = append(out, [2]int{s + offset, e + offset})
		}
	}
	return b.String(), out
}

// hits finds every match in folded text, sorted and merged.
func (q Query) hits(folded []rune) [][2]int {
	var hits [][2]int
	for _, t := range q.Terms {
		term := []rune(t)
		for i := 0; i+len(term) <= len(folded); i++ {
			if (i == 0 || !isWordRune(folded[i-1])) && slices.Equal(folded[i:i+len(term)], term) {
				hits = append(hits, [2]int{i, i + len(term)})
			}
		}
	}
	if len(hits) == 0 && q.Phrase != "" {
		phrase := []rune(q.Phrase)
		for i := 0; i+len(phrase) <= len(folded); i++ {
			if slices.Equal(folded[i:i+len(phrase)], phrase) {
				hits = append(hits, [2]int{i, i + len(phrase)})
			}
		}
	}
	slices.SortFunc(hits, func(a, b [2]int) int { return a[0] - b[0] })
	merged := hits[:0]
	for _, h := range hits {
		if n := len(merged); n > 0 && h[0] <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], h[1])
			continue
		}
		merged = append(merged, h)
	}
	return merged
}

// collapseSpace trims s and turns every whitespace run into one space.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func lastIndex(rs []rune, r rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] == r {
			return i
		}
	}
	return -1
}
