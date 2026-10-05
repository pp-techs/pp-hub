package mdoc

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sync"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// An emoji is `:shortcode:` (GitHub's form), slash-editor's `emoji` node with
// a `name` attribute. Whether `:name:` is an emoji depends on the dataset:
// slash-editor's tokenizer consumes every `:[a-zA-Z0-9_+-]+:` it meets, and
// only then turns a shortcode it does not know back into text. The consuming
// matters even then — `x:https://e.x` is no bare URL, because `:https:` was
// read first.

//go:embed emoji.json
var emojiJSON []byte

// emojiGlyphs maps every shortcode the editor's emoji node knows (each
// entry's name and shortcodes) to its glyph. emoji.json is written by
// apps/web/scripts/mdoc-fixtures.ts from the kit's own dataset.
var emojiGlyphs = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(emojiJSON, &m); err != nil {
		panic("mdoc: emoji.json: " + err.Error())
	}
	return m
})

var shortcodeRe = regexp.MustCompile(`^:([a-zA-Z0-9_+-]+):`)

// shortcodeType is the inline pseudo-node a `:name:` token parses to. It
// never leaves inlines: resolveShortcodes turns it into an emoji or text
// after linkify has decided whether an autolink swallowed it.
const shortcodeType = "\x00shortcode"

var kindShortcode = ast.NewNodeKind("SlashShortcode")

type shortcodeNode struct {
	ast.BaseInline
	name, raw string
}

func (n *shortcodeNode) Kind() ast.NodeKind { return kindShortcode }

func (n *shortcodeNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

// shortcodeParser is the emoji tokenizer as a goldmark inline parser; like
// marked's extensions it runs ahead of the built-in inline parsers.
type shortcodeParser struct{}

func (shortcodeParser) Trigger() []byte { return []byte{':'} }

func (shortcodeParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	m := shortcodeRe.FindSubmatch(line)
	if m == nil {
		return nil
	}
	block.Advance(len(m[0]))
	return &shortcodeNode{name: string(m[1]), raw: string(m[0])}
}

// resolveShortcode is the token's final node: an emoji for a known
// shortcode (an atom, so the marks around it do not reach it, as in
// slash-editor), otherwise its text.
func resolveShortcode(n Node) Node {
	name := attrString(n.Attrs["name"])
	if _, ok := emojiGlyphs()[name]; ok {
		return Node{Type: "emoji", Attrs: map[string]any{"name": name}}
	}
	return Node{Type: "text", Text: n.Text, Marks: n.Marks}
}

func resolveShortcodes(nodes []Node) []Node {
	for i := range nodes {
		if nodes[i].Type == shortcodeType {
			nodes[i] = resolveShortcode(nodes[i])
		}
	}
	return nodes
}

// emojiText is what an emoji node reads as: its glyph, or `:name:` for a
// name the dataset does not know (Tiptap's renderText).
func emojiText(name string) string {
	if g, ok := emojiGlyphs()[name]; ok {
		return g
	}
	return ":" + name + ":"
}
