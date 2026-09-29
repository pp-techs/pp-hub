// Package mdoc converts between markdown files and the ProseMirror document
// slash-editor edits, and between that document and the Y.XmlFragment a
// collaboration room holds. See docs/architecture/block-documents.md.
//
// Everything slash-editor serializes is written byte-identically to its
// serializeMarkdown (the golden fixtures in testdata pin it), including its
// mention marker pair:
//
//	<!-- slash:mention {"id":"user:<uuid>","label":"Ann"} -->@Ann<!-- /slash:mention -->
//
// Parse reads a line that starts with a mention as paragraph text, where
// slash-editor's own parser misreads it.
//
// # Comment marks
//
// slash-editor's serializer drops the comment mark (`{threadId}`, several
// may overlap), so this package persists it on its own, as HTML comments
// GitHub and other renderers hide:
//
//	Hello <!-- slash:comment {"threadId":"T"} -->world<!-- /slash:comment {"threadId":"T"} -->!
//
// An open marker and the next close marker of the same thread enclose the
// commented text; pairs of different threads interleave freely, so
// overlapping and nested threads need nothing more. Markers sit between the
// rendered text nodes, outside any emphasis, link or code delimiters, and
// removing them leaves exactly slash-editor's markdown (a table pads its
// cells as if they were not there). A marker is never written where it
// would start a line (CommonMark would open an HTML block and show the line
// as raw HTML) nor inside a code span: it moves right to the next boundary
// that is neither and records `"back":n`, the number of UTF-16 code units of
// text it moved past, so that
//
//	Hello<!-- slash:comment {"threadId":"T","back":5} --><!-- /slash:comment {"threadId":"T"} --> world
//
// comments "Hello" at the start of a paragraph. Markers pair within one
// textblock; a thread spanning several blocks has a pair in each. Parse
// drops, without flagging the document lossy, a pair enclosing no text, a
// close without an open, an open never closed, a second open of a pending
// thread and a marker with a malformed payload (no string threadId, back not
// a non-negative integer). Text marked code gets no comment mark (code
// excludes every other mark), and marks on inline nodes other than text are
// not stored (y-tiptap does not store them either).
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
