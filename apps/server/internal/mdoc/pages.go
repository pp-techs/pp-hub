package mdoc

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Pages: slash-editor's `subPage` block and inline `pageLink`, both keyed by
// `pageId` (a pp-hub node id). The markdown carries the page's title and
// href only for other readers; Parse keeps nothing but the id and Render
// takes both from its caller (RenderWith):
//
//	<!-- slash:subPage {"pageId":"<id>"} -->
//	[Title](/edit/<id>)
//
//	<!-- slash:pageLink {"pageId":"<id>"} -->[Title](/edit/<id>)<!-- /slash:pageLink -->
//
// A `doc:<id>` mention, the pre-pages way of pointing at a document, parses
// as pageLink{pageId:<id>}.

const pageLinkClose = "<!-- /slash:pageLink -->"

// docMentionPrefix marks a mention of a document rather than a person.
const docMentionPrefix = "doc:"

// subPage is the subPage block tokenizer: a marker line with a string
// pageId, then one link line (not an image) ending its line.
func (p *mdParser) subPage(r string) (Node, int, bool) {
	m := readMarker(r, "subPage")
	if m == nil {
		return Node{}, 0, false
	}
	link := readLinkLine(r[len(m.raw):])
	pageID, _ := m.attrs["pageId"].(string)
	if link == nil || link.image || pageID == "" {
		return Node{}, 0, false
	}
	raw := len(m.raw) + len(link.raw)
	if !blankRest.MatchString(r[raw:]) {
		return Node{}, 0, false
	}
	return Node{Type: "subPage", Attrs: map[string]any{"pageId": pageID}}, raw, true
}

var pageLinkOpen = regexp.MustCompile(`^<!-- slash:pageLink(?: (\{.*?\}))? -->`)

// readPageLink is the pageLink tokenizer at the start of s: the page id and
// the raw length, or ok=false when it declines (malformed payload, no
// string pageId, no close marker on the line). What sits between the marker
// pair (the title link) is not read.
func readPageLink(s []byte) (pageID string, n int, ok bool) {
	m := pageLinkOpen.FindSubmatchIndex(s)
	if m == nil || m[2] < 0 {
		return "", 0, false
	}
	var payload map[string]any
	if err := json.Unmarshal(s[m[2]:m[3]], &payload); err != nil {
		return "", 0, false
	}
	pageID, _ = payload["pageId"].(string)
	if pageID == "" {
		return "", 0, false
	}
	end := bytes.Index(s[m[1]:], []byte(pageLinkClose))
	if end < 0 {
		return "", 0, false
	}
	return pageID, m[1] + end + len(pageLinkClose), true
}

var kindPageLink = ast.NewNodeKind("SlashPageLink")

// pageLinkNode is a pageLink in goldmark's inline tree.
type pageLinkNode struct {
	ast.BaseInline
	pageID string
}

func (n *pageLinkNode) Kind() ast.NodeKind { return kindPageLink }

func (n *pageLinkNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

// pageLinkParser is the pageLink tokenizer as a goldmark inline parser,
// ahead of the raw HTML parser like mentionParser.
type pageLinkParser struct{}

func (pageLinkParser) Trigger() []byte { return []byte{'<'} }

func (pageLinkParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	pageID, n, ok := readPageLink(line)
	if !ok {
		return nil
	}
	block.Advance(n)
	return &pageLinkNode{pageID: pageID}
}

// mentionToNode is the node a parsed mention becomes: a pageLink for a
// `doc:<id>` mention, the mention itself otherwise.
func mentionToNode(attrs map[string]any) Node {
	if id, ok := attrs["id"].(string); ok && strings.HasPrefix(id, docMentionPrefix) && len(id) > len(docMentionPrefix) {
		return Node{Type: "pageLink", Attrs: map[string]any{"pageId": id[len(docMentionPrefix):]}}
	}
	return Node{Type: "mention", Attrs: maps.Clone(attrs)}
}

// PageRefs returns the page ids doc's subPage blocks and pageLink nodes
// point at, at any depth, each list in document order without duplicates.
func PageRefs(doc Node) (subPages, links []string) {
	seenSub, seenLink := map[string]bool{}, map[string]bool{}
	walk(&doc, func(n *Node) {
		id, _ := n.Attrs["pageId"].(string)
		if id == "" {
			return
		}
		switch n.Type {
		case "subPage":
			if !seenSub[id] {
				seenSub[id] = true
				subPages = append(subPages, id)
			}
		case "pageLink":
			if !seenLink[id] {
				seenLink[id] = true
				links = append(links, id)
			}
		}
	})
	return subPages, links
}

// MapPageIDs returns a deep copy of doc with every subPage and pageLink
// pageId replaced by f(pageId); f returning "" keeps the id. doc itself is
// left untouched.
func MapPageIDs(doc Node, f func(id string) string) Node {
	out := doc
	if doc.Attrs != nil {
		out.Attrs = maps.Clone(doc.Attrs)
		if doc.Type == "subPage" || doc.Type == "pageLink" {
			if id, _ := doc.Attrs["pageId"].(string); id != "" {
				if mapped := f(id); mapped != "" {
					out.Attrs["pageId"] = mapped
				}
			}
		}
	}
	if doc.Marks != nil {
		out.Marks = make([]Mark, len(doc.Marks))
		for i, m := range doc.Marks {
			out.Marks[i] = Mark{Type: m.Type, Attrs: maps.Clone(m.Attrs)}
		}
	}
	if doc.Content != nil {
		out.Content = make([]Node, len(doc.Content))
		for i, c := range doc.Content {
			out.Content[i] = MapPageIDs(c, f)
		}
	}
	return out
}
