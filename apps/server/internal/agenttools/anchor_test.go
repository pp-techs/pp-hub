package agenttools

import (
	"errors"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
)

// commented is the text the thread's mark covers, read back from markdown.
func commented(t *testing.T, md []byte, thread string) string {
	t.Helper()
	parsed, err := mdoc.Parse(md)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var walk func(mdoc.Node)
	walk = func(n mdoc.Node) {
		for _, m := range n.Marks {
			if m.Type == "comment" && m.Attrs["threadId"] == thread {
				b.WriteString(n.Text)
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(parsed.Doc)
	return b.String()
}

func TestAnchorComment(t *testing.T) {
	for _, tc := range []struct {
		name, md, quote, want string
	}{
		{"mid sentence", "Hello world, again.\n", "world", "world"},
		// Matched as the text reads: the quote carries no ** and the mark
		// spans the bold and plain halves.
		{"across emphasis", "Ship **the release** on Friday.\n", "release on", "release on"},
		// A marker cannot open a line (CommonMark would start an HTML
		// block); mdoc moves it and records back, and the anchor survives.
		{"line start", "Budget is final.\n", "Budget", "Budget"},
		{"list item", "- first item\n- second item\n", "second", "second"},
		{"vietnamese", "Báo cáo quý ba đã xong.\n", "quý ba", "quý ba"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := AnchorComment([]byte(tc.md), tc.quote, "t1")
			if err != nil {
				t.Fatal(err)
			}
			if got := commented(t, out, "t1"); got != tc.want {
				t.Fatalf("commented %q, want %q\n%s", got, tc.want, out)
			}
		})
	}
}

func TestAnchorCommentKeepsOtherThreads(t *testing.T) {
	md := "Hello <!-- slash:comment {\"threadId\":\"old\"} -->world<!-- /slash:comment {\"threadId\":\"old\"} --> and moon.\n"
	out, err := AnchorComment([]byte(md), "moon", "new")
	if err != nil {
		t.Fatal(err)
	}
	if got := commented(t, out, "old"); got != "world" {
		t.Fatalf("existing thread now covers %q\n%s", got, out)
	}
	if got := commented(t, out, "new"); got != "moon" {
		t.Fatalf("new thread covers %q\n%s", got, out)
	}
}

func TestAnchorCommentRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, md, quote string
	}{
		{"not found", "Hello world.\n", "planet"},
		{"markdown syntax in quote", "Ship **the release**.\n", "**the release**"},
		{"ambiguous", "a cat and a cat\n", "cat"},
		{"ambiguous across blocks", "cat\n\ncat\n", "cat"},
		{"across blocks", "one\n\ntwo\n", "one\n\ntwo"},
		{"code only", "Run `make` now.\n", "make"},
		{"empty", "Hello.\n", "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AnchorComment([]byte(tc.md), tc.quote, "t1")
			if !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("err = %v, want an argument error", err)
			}
		})
	}
}
