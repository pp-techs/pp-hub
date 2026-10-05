package mdoc

// The v1 schema: createMdocKit() in apps/web/src/lib/collab/mdoc-kit.ts.
// TestSchemaMatchesFixtures holds these tables to testdata/schema.json, which
// the fixture generator dumps from the real ProseMirror schema.

// nodeAttrDefaults is every node type's attribute set with its default
// (nil = null). A type with an "id" attribute is id-bearing.
var nodeAttrDefaults = map[string]map[string]any{
	"doc":            {},
	"text":           {},
	"paragraph":      {"id": nil},
	"heading":        {"id": nil, "level": 1.0},
	"blockquote":     {"id": nil},
	"bulletList":     {"id": nil},
	"orderedList":    {"id": nil, "start": 1.0, "type": nil},
	"listItem":       {"id": nil},
	"taskList":       {"id": nil},
	"taskItem":       {"id": nil, "checked": false},
	"codeBlock":      {"id": nil, "language": nil},
	"mermaid":        {"id": nil},
	"horizontalRule": {"id": nil},
	"hardBreak":      {},
	"callout":        {"id": nil, "icon": "💡"},
	"details":        {"id": nil, "open": false, "level": 0.0},
	"detailsSummary": {},
	"detailsContent": {"id": nil},
	"image":          {"id": nil, "src": nil, "alt": nil, "width": nil, "status": "ready", "error": nil},
	"file":           {"id": nil, "src": nil, "name": nil, "size": nil, "mime": nil, "status": "ready", "error": nil},
	"video":          {"id": nil, "src": nil, "poster": nil, "status": "ready", "error": nil},
	"embed":          {"id": nil, "url": nil, "mode": "bookmark", "title": nil, "description": nil, "thumbnail": nil},
	"mention":        {"id": nil, "label": nil},
	"emoji":          {"name": nil},
	"subPage":        {"id": nil, "pageId": nil},
	"pageLink":       {"pageId": nil},
	"table":          {"id": nil},
	"tableRow":       {},
	"tableHeader":    {"id": nil, "colspan": 1.0, "rowspan": 1.0, "colwidth": nil, "align": nil},
	"tableCell":      {"id": nil, "colspan": 1.0, "rowspan": 1.0, "colwidth": nil, "align": nil},
	"columns":        {"id": nil},
	"column":         {"id": nil},
}

// markAttrDefaults is every mark type's attribute set with its default.
var markAttrDefaults = map[string]map[string]any{
	"link":    {"href": nil, "target": "_blank", "rel": "noopener noreferrer nofollow", "class": nil, "title": nil},
	"bold":    {},
	"code":    {},
	"italic":  {},
	"strike":  {},
	"comment": {"threadId": nil},
}

// markOrder is the schema's mark rank order (Tiptap extension priority):
// ProseMirror sorts a node's marks by it, and the serializer nests
// lower-ranked marks outside higher-ranked ones.
var markOrder = []string{"link", "bold", "code", "italic", "strike", "comment"}

func markRank(name string) int {
	for i, m := range markOrder {
		if m == name {
			return i
		}
	}
	return len(markOrder)
}

// codeTypes are the extensions with `code: true`: their text is written
// verbatim, never escaped.
func isCodeType(name string) bool {
	return name == "codeBlock" || name == "mermaid" || name == "code"
}

// hasIDAttr reports whether nodeType carries a BlockId block id. A mention's
// "id" is the mentioned entity (`user:<uuid>`), not a block id; a subPage
// has one (it is a block), a pageLink (inline) has none.
func hasIDAttr(nodeType string) bool {
	if nodeType == "mention" {
		return false
	}
	_, ok := nodeAttrDefaults[nodeType]["id"]
	return ok
}
