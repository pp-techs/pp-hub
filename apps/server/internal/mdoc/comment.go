package mdoc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/reearth/ygo/encoding"
)

// Comment marks (`{threadId}`, overlapping allowed) are persisted by this
// package only; slash-editor's serializer drops them. The format is described
// in the package documentation (mdoc.go).

// commentEdgeType is the placeholder Parse puts in inline content for a
// comment marker until resolveComments turns the pairs into marks. It can
// never be a schema type.
const commentEdgeType = "\x00commentEdge"

var (
	commentMarkerRe     = regexp.MustCompile(`^<!-- (/?)slash:comment (\{.*\}) -->$`)
	commentMarkerPrefix = regexp.MustCompile(`^<!-- /?slash:comment \{.*?\} -->`)
	commentMarkerAny    = regexp.MustCompile(`<!-- /?slash:comment \{.*?\} -->`)
)

// cellWidth is a table cell's width as slash-editor pads it: its length
// without the comment markers, so that removing them leaves slash-editor's
// own table.
func cellWidth(s string) int {
	if strings.Contains(s, "slash:comment") {
		s = commentMarkerAny.ReplaceAllString(s, "")
	}
	return jsLen(s)
}

// parseCommentMarker reads one inline HTML comment as a comment marker: a
// placeholder node, or ok=false when html is not one or its payload is
// malformed (no string threadId, back not a non-negative integer).
func parseCommentMarker(html string) (Node, bool) {
	m := commentMarkerRe.FindStringSubmatch(html)
	if m == nil {
		return Node{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(m[2]), &payload); err != nil {
		return Node{}, false
	}
	id, ok := payload["threadId"].(string)
	if !ok || id == "" {
		return Node{}, false
	}
	back := 0
	if v, has := payload["back"]; has {
		f, isNum := v.(float64)
		if !isNum || f < 0 || f != math.Trunc(f) || f > math.MaxInt32 {
			return Node{}, false
		}
		back = int(f)
	}
	return Node{Type: commentEdgeType, Attrs: map[string]any{
		"threadId": id,
		"open":     m[1] == "",
		"back":     back,
	}}, true
}

// resolveComments replaces the comment marker placeholders under n by
// comment marks on the text they enclose.
func resolveComments(n *Node) {
	edges := false
	for i := range n.Content {
		if n.Content[i].Type == commentEdgeType {
			edges = true
			continue
		}
		resolveComments(&n.Content[i])
	}
	if edges {
		n.Content = applyCommentEdges(n.Content)
	}
}

type commentSpan struct {
	id       string
	from, to int // UTF-16 offsets into the text of the inline content
}

// applyCommentEdges pairs the markers of one inline content in order (an
// open, then the next close of the same thread; a second open while one is
// pending, a close without an open and an open never closed are dropped),
// then marks the text each non-empty pair encloses, except code (the code
// mark excludes every other mark).
func applyCommentEdges(content []Node) []Node {
	var spans []commentSpan
	pending := map[string]int{}
	inline := make([]Node, 0, len(content))
	off := 0
	for _, c := range content {
		if c.Type != commentEdgeType {
			inline = append(inline, c)
			if c.Type == "text" {
				off += jsLen(c.Text)
			}
			continue
		}
		id := c.Attrs["threadId"].(string)
		pos := max(0, off-c.Attrs["back"].(int))
		if c.Attrs["open"].(bool) {
			if _, ok := pending[id]; !ok {
				pending[id] = pos
			}
			continue
		}
		from, ok := pending[id]
		if !ok {
			continue
		}
		delete(pending, id)
		if pos > from {
			spans = append(spans, commentSpan{id, from, pos})
		}
	}
	if len(spans) == 0 {
		return mergeText(inline)
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].from != spans[j].from {
			return spans[i].from < spans[j].from
		}
		return spans[i].id < spans[j].id
	})
	out := make([]Node, 0, len(inline))
	off = 0
	for _, c := range inline {
		if c.Type != "text" || hasMark(c.Marks, "code") {
			out = append(out, c)
			if c.Type == "text" {
				off += jsLen(c.Text)
			}
			continue
		}
		start, end := off, off+jsLen(c.Text)
		off = end
		cuts := []int{start}
		for _, s := range spans {
			for _, at := range []int{s.from, s.to} {
				if at > start && at < end {
					cuts = append(cuts, at)
				}
			}
		}
		cuts = append(cuts, end)
		sort.Ints(cuts)
		for i := 0; i+1 < len(cuts); i++ {
			a, b := cuts[i], cuts[i+1]
			if a == b {
				continue
			}
			piece := c
			piece.Text = utf16Slice(c.Text, a-start, b-start)
			piece.Marks = append([]Mark(nil), c.Marks...)
			for _, s := range spans {
				if s.from <= a && b <= s.to && !hasComment(piece.Marks, s.id) {
					piece.Marks = append(piece.Marks, Mark{Type: "comment", Attrs: map[string]any{"threadId": s.id}})
				}
			}
			out = append(out, piece)
		}
	}
	return mergeText(out)
}

// utf16Slice is s.slice(from, to) in UTF-16 code units; a bound inside a
// surrogate pair moves past it.
func utf16Slice(s string, from, to int) string {
	a, b := len(s), len(s)
	units := 0
	for i, r := range s {
		if a == len(s) && units >= from {
			a = i
		}
		if units >= to {
			b = i
			break
		}
		units += utf16Units(r)
	}
	return s[a:b]
}

func utf16Units(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func hasComment(marks []Mark, id string) bool {
	for _, m := range marks {
		if m.Type == "comment" && m.Attrs["threadId"] == id {
			return true
		}
	}
	return false
}

// commentIDs are the thread ids of marks' comment marks, in order, once.
func commentIDs(marks []Mark) []string {
	var ids []string
	for _, m := range marks {
		if m.Type != "comment" {
			continue
		}
		if id, ok := m.Attrs["threadId"].(string); ok && id != "" && !contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func hasCommentMarks(nodes []Node) bool {
	for i := range nodes {
		if hasMark(nodes[i].Marks, "comment") {
			return true
		}
	}
	return false
}

func withoutComments(marks []Mark) []Mark {
	if !hasMark(marks, "comment") {
		return marks
	}
	out := make([]Mark, 0, len(marks))
	for _, m := range marks {
		if m.Type != "comment" {
			out = append(out, m)
		}
	}
	return out
}

type commentEdge struct {
	id   string
	open bool
	pos  int // UTF-16 offset of the boundary in the inline content's text
	back int
}

// renderCommented renders inline content that carries comment marks: the
// content rendered without them (slash-editor's output), with the markers
// inserted between the rendered nodes. A marker never goes where it would
// start a line (the start of a paragraph, after a newline: CommonMark would
// open an HTML block there and print the line as raw HTML) nor inside a code
// span; it moves right to the next boundary that is neither, and `back`
// records how many UTF-16 units of text it moved past.
func renderCommented(nodes []Node, parent *Node) string {
	stripped := make([]Node, len(nodes))
	for i := range nodes {
		stripped[i] = nodes[i]
		stripped[i].Marks = withoutComments(nodes[i].Marks)
	}
	pieces, _ := renderPieces(stripped, parent)
	n := len(nodes)
	offs := make([]int, n+1)
	for k := range n {
		offs[k+1] = offs[k]
		if nodes[k].Type == "text" {
			offs[k+1] += jsLen(nodes[k].Text)
		}
	}
	safe := func(k int) bool {
		if k == 0 {
			return parent == nil || parent.Type != "paragraph"
		}
		if strings.HasSuffix(pieces[k-1], "\n") {
			return false
		}
		// Inside a code span a marker would be code text. Schema-valid
		// content never needs one there (code excludes comment); this
		// keeps a document from outside the schema from corrupting.
		return k == n || nodes[k-1].Type != "text" || nodes[k].Type != "text" ||
			!hasMark(stripped[k-1].Marks, "code") || !hasMark(stripped[k].Marks, "code")
	}
	slots := make([][]commentEdge, n+1)
	place := func(e commentEdge, k int) {
		for k < n && !safe(k) {
			k++
		}
		e.back = offs[k] - e.pos
		slots[k] = append(slots[k], e)
	}
	var active []string
	after := 0 // the slot right after the last text node
	for k := range n {
		if nodes[k].Type != "text" {
			continue
		}
		ids := commentIDs(nodes[k].Marks)
		kept := active[:0:0]
		for i := len(active) - 1; i >= 0; i-- {
			if !contains(ids, active[i]) {
				place(commentEdge{id: active[i], pos: offs[k]}, after)
			}
		}
		for _, id := range active {
			if contains(ids, id) {
				kept = append(kept, id)
			}
		}
		active = kept
		for _, id := range ids {
			if !contains(active, id) {
				place(commentEdge{id: id, open: true, pos: offs[k]}, after)
				active = append(active, id)
			}
		}
		after = k + 1
	}
	for i := len(active) - 1; i >= 0; i-- {
		place(commentEdge{id: active[i], pos: offs[n]}, after)
	}
	var b strings.Builder
	for k := 0; k <= n; k++ {
		for _, e := range slots[k] {
			b.WriteString(commentMarker(e))
		}
		if k < n {
			b.WriteString(pieces[k])
		}
	}
	return b.String()
}

// commentMarker is `<!-- slash:comment {"threadId":…} -->` (an open) or
// `<!-- /slash:comment {"threadId":…} -->` (a close), plus `"back":n` when
// the marker was written n UTF-16 units after its position.
func commentMarker(e commentEdge) string {
	attrs := orderedAttrs{{"threadId", e.id}}
	if e.back > 0 {
		attrs = append(attrs, attrKV{"back", float64(e.back)})
	}
	payload := strings.ReplaceAll(jsJSON(attrs), "--", `\u002d\u002d`)
	if e.open {
		return "<!-- slash:comment " + payload + " -->"
	}
	return "<!-- /slash:comment " + payload + " -->"
}

// overlappingMarks are the mark types that do not exclude themselves; y-tiptap
// keys their Y text attribute `<name>--<hash>` so several can share a text.
var overlappingMarks = map[string]bool{"comment": true}

// yMarkKey is y-tiptap's Y text attribute key for a mark with the given
// (defaults filled) attributes.
func yMarkKey(typ string, attrs map[string]any) string {
	if !overlappingMarks[typ] {
		return typ
	}
	return typ + "--" + hashOfMarkJSON(typ, attrs)
}

// hashOfMarkJSON is y-tiptap's hashOfJSON(mark.toJSON()): lib0 writeAny of
// {type, attrs} (keys in that order), SHA-256, folded to 6 bytes, base64.
// WriteAny orders the attrs keys, which matches the schema's order for the
// one-attribute comment mark.
func hashOfMarkJSON(typ string, attrs map[string]any) string {
	e := encoding.NewEncoder()
	fields := 1
	if len(attrs) > 0 {
		fields = 2
	}
	e.WriteUint8(118) // object
	e.WriteVarUint(uint64(fields))
	e.WriteVarString("type")
	e.WriteAny(typ)
	if len(attrs) > 0 {
		e.WriteVarString("attrs")
		e.WriteAny(toYValue(attrs))
	}
	digest := sha256.Sum256(e.Bytes())
	const n = 6
	for i := n; i < len(digest); i++ {
		digest[i%n] ^= digest[i]
	}
	return base64.StdEncoding.EncodeToString(digest[:n])
}
