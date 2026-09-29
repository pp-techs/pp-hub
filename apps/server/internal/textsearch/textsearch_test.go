package textsearch

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// Fold is what makes Vietnamese searchable without diacritics, and its
// rune-for-rune shape is what lets a position found in node_texts.folded
// index node_texts.content. Both halves are load-bearing.
func TestFoldVietnamese(t *testing.T) {
	cases := map[string]string{
		"Tiếng Việt":           "tieng viet",
		"ĐƯỜNG ĐI":             "duong di",
		"Hà Nội mùa thu":       "ha noi mua thu",
		"Nguyễn Thị Minh Khai": "nguyen thi minh khai",
		"café naïve":           "cafe naive",
		"ASCII only 123":       "ascii only 123",
		"日本語":                  "日本語",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
		if utf8.RuneCountInString(Fold(in)) != utf8.RuneCountInString(in) {
			t.Errorf("Fold(%q) changed the rune count", in)
		}
	}
}

// Decomposed input (as macOS produces) must fold to the same thing once
// Prepare has composed it — otherwise the same word typed on two machines
// indexes differently.
func TestPrepareComposesBeforeFolding(t *testing.T) {
	decomposed := "Vie\u0302\u0323t"
	content, folded := Prepare(decomposed)
	if content != "Việt" {
		t.Errorf("content = %q, want NFC %q", content, "Việt")
	}
	if folded != "viet" {
		t.Errorf("folded = %q, want %q", folded, "viet")
	}
}

func TestPrepareCapsOnARuneBoundary(t *testing.T) {
	text := strings.Repeat("ệ", MaxIndexedBytes) // 3 bytes each
	content, folded := Prepare(text)
	if len(content) > MaxIndexedBytes || !utf8.ValidString(content) {
		t.Fatalf("content is %d bytes, valid=%v", len(content), utf8.ValidString(content))
	}
	if utf8.RuneCountInString(content) != utf8.RuneCountInString(folded) {
		t.Error("content and folded are no longer character-aligned")
	}
	if c, _ := Prepare("a\x00b\xffc"); c != "abc" {
		t.Errorf("Prepare kept NUL or invalid bytes: %q", c)
	}
}

func TestParseQuery(t *testing.T) {
	q := ParseQuery("  Tiếng   VIỆT, tiếng! ")
	if want := []string{"tieng", "viet"}; !slices.Equal(q.Terms, want) {
		t.Errorf("Terms = %v, want %v", q.Terms, want)
	}
	if q.Phrase != "tieng viet, tieng!" {
		t.Errorf("Phrase = %q", q.Phrase)
	}
	if got := q.TSQuery(); got != "'tieng':* & 'viet':*" {
		t.Errorf("TSQuery = %q", got)
	}
	if got := ParseQuery("--").TSQuery(); got != "" {
		t.Errorf("a query without words must give no tsquery, got %q", got)
	}
}

func TestLikePattern(t *testing.T) {
	if got := ParseQuery("ab").LikePattern(); got != "" {
		t.Errorf("two runes must not enable the substring scan, got %q", got)
	}
	if got := ParseQuery(`50%_a\b`).LikePattern(); got != `50\%\_a\\b` {
		t.Errorf("LikePattern = %q", got)
	}
}

// highlighted returns the snippet text under each highlight.
func highlighted(snippet string, hs [][2]int) []string {
	rs := []rune(snippet)
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, string(rs[h[0]:h[1]]))
	}
	return out
}

func TestSnippetOffsetsAreRunesIntoTheSnippet(t *testing.T) {
	q := ParseQuery("tieng viet")
	s, hs := q.Snippet("Bài học   Tiếng\nViệt cho người mới", false, false)
	if s != "Bài học Tiếng Việt cho người mới" {
		t.Fatalf("snippet = %q", s)
	}
	if want := [][2]int{{8, 13}, {14, 18}}; !slices.Equal(hs, want) {
		t.Fatalf("highlights = %v, want %v", hs, want)
	}
	if got := highlighted(s, hs); !slices.Equal(got, []string{"Tiếng", "Việt"}) {
		t.Errorf("highlighted %v", got)
	}
}

// A hit deep in a long text is shown in context, with ellipses on both cut
// sides counted into the offsets.
func TestSnippetWindowsAroundTheFirstHit(t *testing.T) {
	before := strings.Repeat("lorem ipsum ", 30)
	after := strings.Repeat(" dolor sit amet", 30)
	q := ParseQuery("ngân sách")
	s, hs := q.Snippet(before+"Ngân sách quý hai"+after, true, true)

	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Fatalf("snippet should be cut on both sides: %q", s)
	}
	if n := utf8.RuneCountInString(s); n > SnippetRunes+2 {
		t.Errorf("snippet is %d runes", n)
	}
	if got := highlighted(s, hs); !slices.Equal(got, []string{"Ngân", "sách"}) {
		t.Fatalf("highlighted %v in %q", got, s)
	}
	// Cut on word boundaries: no half word after the leading ellipsis.
	if strings.HasPrefix(s, "…orem") || strings.HasPrefix(s, "…psum") {
		t.Errorf("snippet opens mid-word: %q", s)
	}
}

func TestSnippetHighlightsOnlyWordPrefixes(t *testing.T) {
	q := ParseQuery("port")
	s, hs := q.Snippet("export the report, then port it", false, false)
	if got := highlighted(s, hs); !slices.Equal(got, []string{"port"}) || hs[0][0] != 24 {
		t.Errorf("highlighted %v at %v", got, hs)
	}
	// With no word-start hit, the substring fallback's hits are shown.
	s, hs = ParseQuery("xport").Snippet("export the report", false, false)
	if got := highlighted(s, hs); !slices.Equal(got, []string{"xport"}) {
		t.Errorf("fallback highlighted %v", got)
	}
}

func TestSnippetMergesOverlappingHits(t *testing.T) {
	q := ParseQuery("bud budget")
	_, hs := q.Snippet("the budget", false, false)
	if want := [][2]int{{4, 10}}; !slices.Equal(hs, want) {
		t.Errorf("highlights = %v, want %v", hs, want)
	}
}
