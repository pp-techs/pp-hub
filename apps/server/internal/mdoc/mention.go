package mdoc

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// A mention is slash-editor's inline marker pair around its visible text:
// `<!-- slash:mention {"id":…,"label":…} -->@label<!-- /slash:mention -->`.

const mentionClose = "<!-- /slash:mention -->"

var mentionOpen = regexp.MustCompile(`^<!-- slash:mention(?: (\{.*?\}))? -->`)

// readMention is the mention tokenizer at the start of s: the attributes and
// the raw length, or ok=false when it declines (malformed payload, no close
// on the line, text not starting with "@").
func readMention(s []byte) (attrs map[string]any, n int, ok bool) {
	m := mentionOpen.FindSubmatchIndex(s)
	if m == nil {
		return nil, 0, false
	}
	payload := map[string]any{}
	if m[2] >= 0 {
		var v any
		if err := json.Unmarshal(s[m[2]:m[3]], &v); err != nil {
			return nil, 0, false
		}
		obj, isObj := v.(map[string]any)
		if !isObj {
			return nil, 0, false
		}
		payload = obj
	}
	rest := s[m[1]:]
	end := bytes.Index(rest, []byte(mentionClose))
	if end < 0 {
		return nil, 0, false
	}
	label := rest[:end]
	if !bytes.HasPrefix(label, []byte("@")) || bytes.IndexByte(label, '\n') >= 0 {
		return nil, 0, false
	}
	attrs = map[string]any{"id": nil, "label": string(label[1:])}
	if id, isStr := payload["id"].(string); isStr {
		attrs["id"] = id
	}
	if l, isStr := payload["label"].(string); isStr {
		attrs["label"] = l
	}
	return attrs, m[1] + end + len(mentionClose), true
}

var kindMention = ast.NewNodeKind("SlashMention")

// mentionNode is a mention in goldmark's inline tree.
type mentionNode struct {
	ast.BaseInline
	attrs map[string]any
}

func (n *mentionNode) Kind() ast.NodeKind { return kindMention }

func (n *mentionNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

// mentionParser is the mention tokenizer as a goldmark inline parser; it
// runs before goldmark's raw HTML parser, as marked runs extensions first.
type mentionParser struct{}

func (mentionParser) Trigger() []byte { return []byte{'<'} }

func (mentionParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	attrs, n, ok := readMention(line)
	if !ok {
		return nil
	}
	block.Advance(n)
	return &mentionNode{attrs: attrs}
}

// startsInlineRun reports whether line (up to three spaces of indentation,
// then) opens with inline content led by a marker: a mention, or a comment
// marker followed by more on the same line. Such a line is paragraph text,
// neither an HTML block nor a block marker line. slash-editor's own parser
// gets this wrong (a paragraph that starts with a mention becomes literal
// HTML, one that is only a mention vanishes), so Parse deviates from it here
// to read back what Render writes.
func startsInlineRun(line []byte) bool {
	for i := 0; i < 3 && len(line) > 0 && line[0] == ' '; i++ {
		line = line[1:]
	}
	if _, _, ok := readMention(line); ok {
		return true
	}
	if m := commentMarkerPrefix.FindIndex(line); m != nil {
		if _, ok := parseCommentMarker(string(line[:m[1]])); ok {
			return len(bytes.TrimRight(line[m[1]:], " \t\r\n")) > 0
		}
	}
	return false
}

// htmlBlockParser is goldmark's HTML block parser, except that a line
// startsInlineRun accepts stays paragraph text.
type htmlBlockParser struct{ parser.BlockParser }

func (b htmlBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	if line, _ := reader.PeekLine(); startsInlineRun(line) {
		return nil, parser.NoChildren
	}
	return b.BlockParser.Open(parent, reader, pc)
}

// newParser is goldmark's default parser with htmlBlockParser in place of
// the HTML block parser and mentionParser ahead of the inline parsers.
func newParser() parser.Parser {
	blocks := parser.DefaultBlockParsers()
	html := parser.NewHTMLBlockParser()
	for i := range blocks {
		if blocks[i].Value == html {
			blocks[i].Value = htmlBlockParser{html}
		}
	}
	return parser.NewParser(
		parser.WithBlockParsers(blocks...),
		parser.WithInlineParsers(append(parser.DefaultInlineParsers(), util.Prioritized(mentionParser{}, 90))...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	)
}
