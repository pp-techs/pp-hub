package mdoc

import (
	"math"
	"regexp"
	"sort"

	"github.com/reearth/ygo/crdt"
)

// The Y.XmlFragment encoding is @tiptap/y-tiptap's (prosemirrorToYXmlFragment
// and yXmlFragmentToProsemirrorJSON):
//
//   - every non-text node is a Y.XmlElement named after its type, carrying
//     its schema attributes (defaults filled in, as Node.fromJSON does) minus
//     the null ones, values typed as lib0 writes JS values;
//   - every run of adjacent text nodes is one Y.XmlText whose delta has one
//     insert per text node, formatted with one attribute per mark: key = mark
//     name, value = the mark's full attribute object (`{}` for none). y-tiptap
//     suffixes the key with `--<hash>` only for marks that do not exclude
//     themselves (comment), none of which is in the v1 set; Read strips such
//     a suffix all the same.

// Fill appends doc.Content into frag using @tiptap/y-tiptap's encoding. Call
// inside Transact.
func Fill(txn *crdt.Transaction, frag *crdt.YXmlFragment, doc Node) {
	appendChildren(txn, frag, doc.Content)
}

// xmlParent is what YXmlFragment and YXmlElement share for appending.
type xmlParent interface {
	Len() int
	InsertElement(txn *crdt.Transaction, index int, elem *crdt.YXmlElement)
	InsertText(txn *crdt.Transaction, index int, txt *crdt.YXmlText)
}

// appendChildren appends content to parent. Into a detached element (txn
// nil) the children are buffered and materialise when it is inserted into
// an attached parent, children before attributes, as in Yjs.
func appendChildren(txn *crdt.Transaction, parent xmlParent, content []Node) {
	for i := 0; i < len(content); {
		if content[i].Type != "text" {
			parent.InsertElement(txn, parent.Len(), yElement(content[i]))
			i++
			continue
		}
		j := i
		for j < len(content) && content[j].Type == "text" {
			j++
		}
		if t := yText(content[i:j]); t != nil {
			parent.InsertText(txn, parent.Len(), t)
		}
		i = j
	}
}

func yElement(n Node) *crdt.YXmlElement {
	el := crdt.NewYXmlElement(n.Type)
	attrs := withDefaults(n.Attrs, nodeAttrDefaults[n.Type])
	for _, k := range sortedKeys(attrs) {
		if v := attrs[k]; v != nil {
			el.SetAttributeValue(nil, k, toYValue(v))
		}
	}
	appendChildren(nil, el, n.Content)
	return el
}

func yText(nodes []Node) *crdt.YXmlText {
	delta := make([]crdt.Delta, 0, len(nodes))
	for _, n := range nodes {
		if n.Text == "" {
			continue
		}
		var attrs crdt.Attributes
		if len(n.Marks) > 0 {
			attrs = make(crdt.Attributes, len(n.Marks))
			for _, m := range n.Marks {
				attrs[m.Type] = toYValue(withDefaults(m.Attrs, markAttrDefaults[m.Type]))
			}
		}
		delta = append(delta, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: n.Text, Attributes: attrs})
	}
	if len(delta) == 0 {
		return nil
	}
	t := crdt.NewYXmlText()
	t.ApplyDelta(nil, delta)
	return t
}

// withDefaults is ProseMirror's computeAttrs: the schema's attributes with
// defaults for the missing ones. A type unknown to the schema keeps attrs.
func withDefaults(attrs map[string]any, defaults map[string]any) map[string]any {
	if defaults == nil {
		defaults = map[string]any{}
		if attrs != nil {
			return attrs
		}
	}
	out := make(map[string]any, len(defaults))
	for k, def := range defaults {
		if v, ok := attrs[k]; ok {
			out[k] = v
		} else {
			out[k] = def
		}
	}
	return out
}

// toYValue converts a decoded JSON value to what lib0's writeAny emits for
// the equivalent JS value: integral numbers within 31 bits as integers.
func toYValue(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) <= math.MaxInt32 {
			return int64(x)
		}
		return x
	case int:
		return int64(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = toYValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = toYValue(e)
		}
		return out
	}
	return v
}

// fromYValue converts a value read from Y to the encoding/json shape
// (every number float64).
func fromYValue(v any) any {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float32:
		return float64(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = fromYValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = fromYValue(e)
		}
		return out
	}
	return v
}

// Read converts frag back to a doc Node. Call outside Transact.
func Read(frag *crdt.YXmlFragment) Node {
	doc := Node{Type: "doc"}
	for _, c := range frag.Children() {
		doc.Content = append(doc.Content, readChild(c)...)
	}
	return doc
}

func readChild(c any) []Node {
	switch x := c.(type) {
	case *crdt.YXmlElement:
		n := Node{Type: x.NodeName}
		if vals := x.GetAttributeValues(); len(vals) > 0 {
			n.Attrs = make(map[string]any, len(vals))
			for k, v := range vals {
				n.Attrs[k] = fromYValue(v)
			}
		}
		for _, cc := range x.Children() {
			n.Content = append(n.Content, readChild(cc)...)
		}
		return []Node{n}
	case *crdt.YXmlText:
		return readText(x)
	}
	return nil
}

var hashedMarkName = regexp.MustCompile(`^(.*)--[a-zA-Z0-9+/=]{8}$`)

func readText(t *crdt.YXmlText) []Node {
	var out []Node
	for _, d := range t.ToDelta() {
		s, ok := d.Insert.(string)
		if !ok || s == "" {
			continue
		}
		n := Node{Type: "text", Text: s}
		for k, v := range d.Attributes {
			if v == nil {
				continue
			}
			name := k
			if m := hashedMarkName.FindStringSubmatch(k); m != nil {
				name = m[1]
			}
			mark := Mark{Type: name}
			if attrs, ok := fromYValue(v).(map[string]any); ok && len(attrs) > 0 {
				mark.Attrs = attrs
			}
			n.Marks = append(n.Marks, mark)
		}
		sort.SliceStable(n.Marks, func(i, j int) bool {
			ri, rj := markRank(n.Marks[i].Type), markRank(n.Marks[j].Type)
			if ri != rj {
				return ri < rj
			}
			return n.Marks[i].Type < n.Marks[j].Type
		})
		out = append(out, n)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
