package agenttools

import (
	"errors"
	"slices"
	"strings"

	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
)

// inlineAtom stands in for an inline node that is not text (a mention, a
// hard break, an image) when a textblock is flattened for matching, so a
// quote can neither match across one nor match its placeholder.
const inlineAtom = "\uFFFC"

// AnchorComment marks quote in a markdown document with thread's comment
// mark and renders the document back. The quote is matched against the text
// as it reads — markdown syntax removed — inside one textblock, and must
// occur exactly once in the document.
//
// The work happens on the parsed tree rather than on the markdown source:
// where a marker may sit (never at a line start, never inside a code span,
// `back` when it has to move) is mdoc's to decide, and mdoc.Render is the
// serializer the editor's own write-back uses. The cost is the one every
// write-back already pays: the file comes back in mdoc's canonical form.
func AnchorComment(src []byte, quote, thread string) ([]byte, error) {
	if strings.TrimSpace(quote) == "" {
		return nil, badArgs("quote is empty")
	}
	if strings.Contains(quote, "\n") {
		return nil, badArgs("quote must sit inside one paragraph, heading or list item")
	}
	parsed, err := mdoc.Parse(src)
	if err != nil {
		return nil, err
	}
	if parsed.Lossy {
		return nil, errors.New("this document has content the editor cannot hold, so it cannot be anchored here; comment on it in pp-hub")
	}
	var hits []*mdoc.Node
	var starts []int
	walkTextblocks(&parsed.Doc, func(block *mdoc.Node) {
		flat := flatten(block)
		for from := 0; ; {
			i := strings.Index(flat[from:], quote)
			if i < 0 {
				return
			}
			hits = append(hits, block)
			starts = append(starts, from+i)
			from += i + len(quote)
		}
	})
	switch len(hits) {
	case 0:
		return nil, badArgs("quote not found; give the text exactly as it reads, without markdown syntax such as ** or #, inside one paragraph")
	case 1:
	default:
		return nil, badArgs("quote appears %d times; quote more of the sentence so it appears once", len(hits))
	}
	if !markRange(hits[0], starts[0], starts[0]+len(quote), thread) {
		return nil, badArgs("quote is code, which cannot carry a comment; quote the text around it")
	}
	out := mdoc.Render(parsed.Doc, parsed.Frontmatter)

	// The renderer decides where markers may go; make sure the thread made
	// it into the file before anybody writes it.
	check, err := mdoc.Parse(out)
	if err != nil {
		return nil, err
	}
	if !hasThread(check.Doc, thread) {
		return nil, errors.New("the comment could not be anchored at that text; quote a plainer stretch of it")
	}
	return out, nil
}

// walkTextblocks calls fn for every node whose children include text.
func walkTextblocks(n *mdoc.Node, fn func(*mdoc.Node)) {
	if slices.ContainsFunc(n.Content, func(c mdoc.Node) bool { return c.Type == "text" }) {
		fn(n)
		return
	}
	for i := range n.Content {
		walkTextblocks(&n.Content[i], fn)
	}
}

// flatten is a textblock's text, inline atoms replaced by inlineAtom.
func flatten(block *mdoc.Node) string {
	var b strings.Builder
	for _, c := range block.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		} else {
			b.WriteString(inlineAtom)
		}
	}
	return b.String()
}

// markRange adds the comment mark to the text in [from, to) of block's
// flattened text, splitting text nodes at the edges. Code-marked text is
// left alone, as the schema requires; it reports whether anything was marked.
func markRange(block *mdoc.Node, from, to int, thread string) bool {
	comment := mdoc.Mark{Type: "comment", Attrs: map[string]any{"threadId": thread}}
	out := make([]mdoc.Node, 0, len(block.Content)+2)
	marked := false
	off := 0
	for _, c := range block.Content {
		width := len(inlineAtom)
		if c.Type == "text" {
			width = len(c.Text)
		}
		start, end := off, off+width
		off = end
		if c.Type != "text" || end <= from || start >= to || hasMark(c.Marks, "code") {
			out = append(out, c)
			continue
		}
		a, b := max(from, start)-start, min(to, end)-start
		if a > 0 {
			out = append(out, textWith(c, c.Text[:a], c.Marks))
		}
		out = append(out, textWith(c, c.Text[a:b], append(slices.Clone(c.Marks), comment)))
		marked = true
		if b < len(c.Text) {
			out = append(out, textWith(c, c.Text[b:], c.Marks))
		}
	}
	block.Content = out
	return marked
}

func textWith(n mdoc.Node, text string, marks []mdoc.Mark) mdoc.Node {
	n.Text = text
	n.Marks = slices.Clone(marks)
	return n
}

func hasMark(marks []mdoc.Mark, typ string) bool {
	return slices.ContainsFunc(marks, func(m mdoc.Mark) bool { return m.Type == typ })
}

func hasThread(n mdoc.Node, thread string) bool {
	for _, m := range n.Marks {
		if m.Type == "comment" && m.Attrs["threadId"] == thread {
			return true
		}
	}
	return slices.ContainsFunc(n.Content, func(c mdoc.Node) bool { return hasThread(c, thread) })
}
