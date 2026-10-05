package mdoc

import "strings"

// AttachmentScheme prefixes an attachment's blob hash wherever a document
// references one: `attachment:<hash>`.
const AttachmentScheme = "attachment:"

// attachmentAttrs are the attributes that may hold an `attachment:<hash>`
// reference, per node type.
var attachmentAttrs = map[string][]string{
	"image": {"src"},
	"file":  {"src"},
	"video": {"src", "poster"},
	"embed": {"thumbnail"},
}

// attachmentHash is the hash v references, "" when v is not an
// `attachment:<hash>` string.
func attachmentHash(v any) string {
	s, ok := v.(string)
	if !ok || !strings.HasPrefix(s, AttachmentScheme) {
		return ""
	}
	return s[len(AttachmentScheme):]
}

// walk calls fn on n and every node under it, parents first.
func walk(n *Node, fn func(*Node)) {
	fn(n)
	for i := range n.Content {
		walk(&n.Content[i], fn)
	}
}

// Attachments returns the blob hashes doc references through
// `attachment:<hash>` (image, file and video sources, video posters, embed
// thumbnails), in document order without duplicates.
func Attachments(doc Node) []string {
	var out []string
	seen := map[string]bool{}
	walk(&doc, func(n *Node) {
		for _, k := range attachmentAttrs[n.Type] {
			if h := attachmentHash(n.Attrs[k]); h != "" && !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	})
	return out
}

// MapAttachments returns a copy of doc with every `attachment:<hash>`
// reference replaced by fn(hash); doc itself is left untouched.
func MapAttachments(doc Node, fn func(hash string) string) Node {
	out := doc
	if keys := attachmentAttrs[doc.Type]; len(keys) > 0 && doc.Attrs != nil {
		attrs := make(map[string]any, len(doc.Attrs))
		for k, v := range doc.Attrs {
			attrs[k] = v
		}
		for _, k := range keys {
			if h := attachmentHash(attrs[k]); h != "" {
				attrs[k] = fn(h)
			}
		}
		out.Attrs = attrs
	}
	if doc.Content != nil {
		out.Content = make([]Node, len(doc.Content))
		for i, c := range doc.Content {
			out.Content[i] = MapAttachments(c, fn)
		}
	}
	return out
}

// Mentions returns the ids of doc's mention nodes (`user:<uuid>`; a parsed
// `doc:<uuid>` mention is a pageLink, see PageRefs) in document order
// without duplicates.
func Mentions(doc Node) []string {
	var out []string
	seen := map[string]bool{}
	walk(&doc, func(n *Node) {
		if n.Type != "mention" {
			return
		}
		if id, ok := n.Attrs["id"].(string); ok && id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	})
	return out
}

// PlainText returns doc's searchable text: every textblock (paragraph,
// heading, code block, diagram source, toggle title, table cell paragraph,
// ...) on its own line, hard breaks as newlines, mentions as `@label`, plus
// image alt texts, file names and bookmark titles and descriptions. Page
// links and sub-pages add nothing (their titles are not in the document).
// Empty blocks are skipped; there is no trailing newline.
func PlainText(doc Node) string {
	var lines []string
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			lines = append(lines, s)
		}
	}
	var visit func(n *Node)
	visit = func(n *Node) {
		switch n.Type {
		case "paragraph", "heading", "codeBlock", "mermaid", "detailsSummary":
			var b strings.Builder
			for i := range n.Content {
				writeInlineText(&b, &n.Content[i])
			}
			add(b.String())
			return
		case "image":
			add(attrString(n.Attrs["alt"]))
		case "file":
			add(attrString(n.Attrs["name"]))
		case "embed":
			add(attrString(n.Attrs["title"]))
			add(attrString(n.Attrs["description"]))
		}
		for i := range n.Content {
			visit(&n.Content[i])
		}
	}
	visit(&doc)
	return strings.Join(lines, "\n")
}

func writeInlineText(b *strings.Builder, n *Node) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteByte('\n')
	case "mention":
		b.WriteByte('@')
		b.WriteString(attrString(n.Attrs["label"]))
	case "emoji":
		b.WriteString(emojiText(attrString(n.Attrs["name"])))
	}
}
