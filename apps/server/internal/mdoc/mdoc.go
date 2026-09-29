// Package mdoc converts between markdown files and the ProseMirror document
// slash-editor edits, and between that document and the Y.XmlFragment a
// collaboration room holds. See docs/architecture/block-documents.md.
package mdoc

import (
	"github.com/reearth/ygo/crdt"
)

// FragmentName is the Y.XmlFragment slash-editor's collaboration() binds to.
const FragmentName = "content"

// Node is a ProseMirror JSON node, the shape slash-editor's
// parseMarkdown/getJSON produce.
type Node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
}

// Mark is a ProseMirror JSON mark.
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Parsed is a markdown file ready to seed a room.
type Parsed struct {
	// Doc has type "doc"; every id-bearing block has a fresh attrs["id"] and
	// the document ends with an empty paragraph (with id).
	Doc Node
	// Frontmatter is the leading YAML frontmatter block verbatim (delimiters
	// and trailing newline), nil if none.
	Frontmatter []byte
	// Lossy reports constructs outside the v1 block set (raw HTML other than
	// slash markers and <details>, footnotes, ...).
	Lossy bool
}

// Seed returns a full-state V1 update for a fresh doc holding doc.
func Seed(doc Node) ([]byte, error) {
	d := crdt.New()
	frag := d.GetXmlFragment(FragmentName)
	d.Transact(func(txn *crdt.Transaction) { Fill(txn, frag, doc) })
	return crdt.EncodeStateAsUpdateV1(d, nil), nil
}
