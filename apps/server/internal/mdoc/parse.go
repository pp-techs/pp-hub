package mdoc

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Parse converts markdown into a document the way slash-editor's
// parseMarkdown does, then gives every id-bearing node a fresh id and ends
// the document with an empty paragraph.
//
// Block structure follows marked, which slash-editor parses with: at every
// block start the slash-editor tokenizers (markers, media lines, callouts,
// toggles, columns, task and ordered lists; see syntax.go) are tried first,
// and anything else is read from goldmark's GFM tree. Container bodies
// (quotes, list items, toggles, columns, callouts) are re-parsed from their
// dedented text, as marked lexes them, so the slash tokenizers apply at any
// depth. Blank-line runs become empty paragraphs exactly as
// MarkdownManager's parseImplicitEmptyParagraphs counts them.
func Parse(src []byte) (Parsed, error) {
	fm, body := splitFrontmatter(src)
	s := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(body))
	p := &mdParser{refs: collectReferences([]byte(s))}
	content := p.blocks(s, true)
	content = append(content, Node{Type: "paragraph", Content: []Node{}})
	doc := Node{Type: "doc", Content: content}
	assignIDs(&doc)
	var frontmatter []byte
	if fm != nil {
		frontmatter = append([]byte(nil), fm...)
	}
	return Parsed{Doc: doc, Frontmatter: frontmatter, Lossy: p.lossy}, nil
}

var frontmatterRe = regexp.MustCompile(`\A---[ \t]*\r?\n(?:[\s\S]*?\r?\n)?(?:---|\.\.\.)[ \t]*(?:\r?\n|\z)`)

// splitFrontmatter separates a leading YAML frontmatter block (delimiters
// and trailing newline included) from the markdown after it.
func splitFrontmatter(src []byte) (fm, body []byte) {
	loc := frontmatterRe.FindIndex(src)
	if loc == nil {
		return nil, src
	}
	return src[:loc[1]], src[loc[1]:]
}

var gm = goldmark.New(goldmark.WithExtensions(
	extension.Table, extension.Strikethrough, extension.TaskList, extension.Footnote,
))

// inlineGM parses text as nothing but paragraphs: marked's inlineTokens.
var inlineGM = goldmark.New(
	goldmark.WithParserOptions(
		parser.WithBlockParsers(util.Prioritized(parser.NewParagraphParser(), 1000)),
		parser.WithParagraphTransformers(),
	),
	goldmark.WithExtensions(extension.Strikethrough),
)

func collectReferences(src []byte) []parser.Reference {
	ctx := parser.NewContext()
	gm.Parser().Parse(text.NewReader(src), parser.WithContext(ctx))
	return ctx.References()
}

type mdParser struct {
	refs  []parser.Reference
	lossy bool
	// nested is marked's `!state.top`: inside a list item, where marked
	// lexes paragraph text line by line, so a block tokenizer can start at
	// any line (blockquotes reset it for their content).
	nested bool
	// inLink is marked's state.inLink: no autolinks inside a link's text.
	inLink bool
}

func (p *mdParser) parse(md goldmark.Markdown, src []byte) ast.Node {
	ctx := parser.NewContext()
	for _, r := range p.refs {
		ctx.AddReference(r)
	}
	return md.Parser().Parse(text.NewReader(src), parser.WithContext(ctx))
}

// entry is one block token: start offset in the text being parsed, whether
// its raw span swallows its last newline (marked tokens that do count one
// newline fewer in the following blank run), and the nodes it produced
// (none for markers and link definitions).
type entry struct {
	start    int
	fullLine bool
	nodes    []Node
}

// blocks parses src as a sequence of blocks. implicit turns blank-line runs
// into empty paragraphs (parseBlockChildren).
func (p *mdParser) blocks(src string, implicit bool) []Node {
	entries := p.entries(src)
	var out []Node
	if !implicit {
		for _, e := range entries {
			out = append(out, e.nodes...)
		}
		return out
	}
	if len(entries) == 0 {
		return emptyParagraphs(countSeparators(src))
	}
	out = append(out, emptyParagraphs(countSeparators(src[:lineStart(src, entries[0].start)]))...)
	for i, e := range entries {
		out = append(out, e.nodes...)
		var gap string
		if i+1 < len(entries) {
			gap = gapBefore(src, lineStart(src, entries[i+1].start), e.start)
		} else {
			gap = gapBefore(src, len(src), e.start)
		}
		if e.fullLine && strings.HasPrefix(gap, "\n") {
			gap = gap[1:]
		}
		n := countSeparators(gap)
		if i+1 < len(entries) {
			n--
		}
		out = append(out, emptyParagraphs(n)...)
	}
	return out
}

func emptyParagraphs(n int) []Node {
	var out []Node
	for range n {
		out = append(out, Node{Type: "paragraph", Content: []Node{}})
	}
	return out
}

func countSeparators(s string) int { return strings.Count(s, "\n\n") }

func lineStart(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return strings.LastIndexByte(src[:off], '\n') + 1
}

// gapBefore returns the blank run ending at end (a line start or len(src)):
// from the newline closing the last non-blank line to end, never reaching
// back before floor.
func gapBefore(src string, end, floor int) string {
	pos := end
	for pos > floor {
		ls := lineStart(src, pos-1)
		if strings.TrimLeft(src[ls:pos], " \t\n") != "" {
			break
		}
		pos = ls
	}
	// pos is the start of the first blank line (or of the block's own last
	// line); the gap begins with the newline that ends the line before it.
	if pos > 0 && pos <= len(src) && src[pos-1] == '\n' && pos-1 >= floor {
		return src[pos-1 : end]
	}
	return src[pos:end]
}

// entries lexes src into block tokens.
func (p *mdParser) entries(src string) []entry {
	var out []entry
	pos := 0
	for pos < len(src) {
		doc := p.parse(gm, []byte(src[pos:]))
		sub := src[pos:]
		children := blockChildren(doc)
		if len(children) == 0 {
			break
		}
		restart := -1
		for i := 0; i < len(children); {
			c := children[i]
			if _, isFootnotes := c.(*east.FootnoteList); isFootnotes {
				out = append(out, entry{start: len(src), nodes: p.convertBlock(c, sub, "", doc)})
				i++
				continue
			}
			start := pos + lineStart(sub, nodeStart(c, []byte(sub)))
			if e, n, ok := p.custom(src, start); ok {
				out = append(out, e...)
				next := start + n
				j := i + 1
				for j < len(children) && pos+nodeStart(children[j], []byte(sub)) < next {
					j++
				}
				if j < len(children) && strings.TrimSpace(src[next:pos+lineStart(sub, nodeStart(children[j], []byte(sub)))]) == "" {
					i = j
					continue
				}
				if j >= len(children) && strings.TrimSpace(src[min(next, len(src)):]) == "" {
					return out
				}
				restart = next
				break
			}
			if cut := p.paragraphCut(c, src, pos); cut > 0 {
				for _, e := range p.entries(src[start:cut]) {
					e.start += start
					out = append(out, e)
				}
				restart = cut
				break
			}
			var end int
			if i+1 < len(children) {
				end = pos + lineStart(sub, nodeStart(children[i+1], []byte(sub)))
			} else {
				end = len(src)
			}
			out = append(out, entry{start: start, nodes: p.convertBlock(c, sub, src[start:end], doc)})
			i++
		}
		if restart < 0 {
			break
		}
		pos = restart
	}
	return out
}

// paragraphCut is where a nested paragraph ends early (0 if it does not):
// the first continuation line at which a slash-editor block tokenizer
// matches. src is the text being parsed, pos the offset goldmark parsed
// from.
func (p *mdParser) paragraphCut(n ast.Node, src string, pos int) int {
	if !p.nested {
		return 0
	}
	switch n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
	default:
		return 0
	}
	sub := src[pos:]
	lines := n.Lines()
	lossy := p.lossy
	defer func() { p.lossy = lossy }()
	for i := 1; i < lines.Len(); i++ {
		at := pos + lineStart(sub, lines.At(i).Start)
		if _, _, ok := p.custom(src, at); ok {
			return at
		}
	}
	return 0
}

func blockChildren(n ast.Node) []ast.Node {
	var out []ast.Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, c)
	}
	return out
}

// nodeStart is a block's start offset in src.
func nodeStart(n ast.Node, src []byte) int {
	if pos := n.Pos(); pos >= 0 {
		return pos
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		return n.Lines().At(0).Start
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if s := nodeStart(c, src); s >= 0 {
			return s
		}
	}
	return len(src)
}

// custom tries slash-editor's block tokenizers at src[start:], in marked's
// order (latest registered first). n is the raw length consumed.
func (p *mdParser) custom(src string, start int) (entries []entry, n int, ok bool) {
	r := src[start:]
	one := func(node Node, raw int, full bool) ([]entry, int, bool) {
		return []entry{{start: start, fullLine: full, nodes: []Node{node}}}, raw, true
	}
	if node, raw, ok := p.columns(r); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.media(r, "file", "name", []string{"size", "mime"}, false); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.media(r, "image", "alt", []string{"width"}, true); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.details(r); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.callout(r); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.taskList(r); ok {
		return one(node, raw, true)
	}
	if node, raw, ok := p.orderedList(r); ok {
		return one(node, raw, false)
	}
	if m := anyMarkerLine.FindString(r); m != "" {
		p.lossy = true
		return []entry{{start: start, fullLine: true}}, len(m), true
	}
	return nil, 0, false
}

func (p *mdParser) columns(r string) (Node, int, bool) {
	open := readMarker(r, "columns")
	if open == nil {
		return Node{}, 0, false
	}
	rest := r[len(open.raw):]
	length, parts, ok := scanBlock(rest, columnsSyntax)
	if !ok || len(parts) < 2 {
		return Node{}, 0, false
	}
	cols := make([]Node, len(parts))
	for i, part := range parts {
		cols[i] = Node{Type: "column", Content: nonEmptyBlocks(p.blocks(part, true))}
	}
	return Node{Type: "columns", Content: cols}, len(open.raw) + length, true
}

// nonEmptyBlocks is blockChildren: a block+ container is never empty.
func nonEmptyBlocks(content []Node) []Node {
	if len(content) == 0 {
		return []Node{{Type: "paragraph", Content: []Node{}}}
	}
	return content
}

func (p *mdParser) media(r, typ, labelAttr string, markerKeys []string, image bool) (Node, int, bool) {
	m := readMarker(r, typ)
	if m == nil && !image {
		return Node{}, 0, false
	}
	markerLen := 0
	if m != nil {
		markerLen = len(m.raw)
	}
	link := readLinkLine(r[markerLen:])
	if link == nil || link.image != image {
		return Node{}, 0, false
	}
	raw := markerLen + len(link.raw)
	if !blankRest.MatchString(r[raw:]) {
		return Node{}, 0, false
	}
	attrs := map[string]any{"src": link.href}
	if m != nil {
		for _, k := range markerKeys {
			if v, ok := m.attrs[k]; ok {
				attrs[k] = v
			}
		}
	}
	if link.label != "" && link.label != link.href {
		attrs[labelAttr] = link.label
	} else {
		attrs[labelAttr] = nil
	}
	return Node{Type: typ, Attrs: attrs}, raw, true
}

func (p *mdParser) details(r string) (Node, int, bool) {
	m := detailsOpen.FindStringSubmatchIndex(r)
	if m == nil {
		return Node{}, 0, false
	}
	rest := r[m[1]:]
	length, parts, ok := scanBlock(rest, detailsSyntax)
	if !ok {
		return Node{}, 0, false
	}
	summary := r[m[4]:m[5]]
	level := 0.0
	if h := detailsHeading.FindStringSubmatch(summary); h != nil && h[1] == h[3] && !strings.Contains(h[2], "</h"+h[1]+">") {
		summary = h[2]
		switch h[1] {
		case "1":
			level = 1
		case "2":
			level = 2
		case "3":
			level = 3
		}
	}
	return Node{
		Type:  "details",
		Attrs: map[string]any{"open": m[2] >= 0, "level": level},
		Content: []Node{
			{Type: "detailsSummary", Content: p.inlineText(summary)},
			{Type: "detailsContent", Content: nonEmptyBlocks(p.blocks(parts[0], true))},
		},
	}, m[1] + length, true
}

func (p *mdParser) callout(r string) (Node, int, bool) {
	mk := readMarker(r, "callout")
	rest := r
	if mk != nil {
		rest = r[len(mk.raw):]
	}
	m := alertRe.FindStringSubmatchIndex(rest)
	if m == nil {
		return Node{}, 0, false
	}
	body := alertBodyPrefix.ReplaceAllString(rest[m[6]:m[7]], "")
	icon := iconByAlert[strings.ToUpper(rest[m[4]:m[5]])]
	if mk != nil {
		if s, ok := mk.attrs["icon"].(string); ok {
			icon = s
		}
	}
	raw := m[1]
	if mk != nil {
		raw += len(mk.raw)
	}
	return Node{
		Type:    "callout",
		Attrs:   map[string]any{"icon": icon},
		Content: nonEmptyBlocks(p.blocks(body, true)),
	}, raw, true
}

func (p *mdParser) taskList(r string) (Node, int, bool) {
	if !taskStartRe.MatchString(r) {
		return Node{}, 0, false
	}
	node, raw := p.taskListFrom(r)
	if raw == 0 {
		return Node{}, 0, false
	}
	return node, min(raw, len(r)), true
}

func (p *mdParser) taskListFrom(r string) (Node, int) {
	items, raw := parseIndentedTasks(r)
	if len(items) == 0 {
		return Node{}, 0
	}
	list := Node{Type: "taskList", Content: make([]Node, 0, len(items))}
	for _, it := range items {
		content := []Node{{Type: "paragraph", Content: p.inlineText(it.main)}}
		if it.nested != "" {
			content = append(content, p.nestedTasks(it.nested)...)
		}
		list.Content = append(list.Content, Node{Type: "taskItem", Attrs: map[string]any{"checked": it.checked}, Content: content})
	}
	return list, raw
}

// nestedTasks is TaskList's customNestedParser.
func (p *mdParser) nestedTasks(content string) []Node {
	list, raw := p.taskListFrom(content)
	if raw == 0 {
		return p.blocks(content, false)
	}
	out := []Node{list}
	if raw < len(content) {
		if rest := content[raw:]; jsTrim(rest) != "" {
			out = append(out, p.blocks(rest, false)...)
		}
	}
	return out
}

func (p *mdParser) orderedList(r string) (Node, int, bool) {
	lines := strings.Split(r, "\n")
	items, consumed := collectOrderedListItems(lines)
	if len(items) == 0 {
		return Node{}, 0, false
	}
	list := p.buildOrdered(items, items[0].indent)
	if len(list.Content) == 0 {
		return Node{}, 0, false
	}
	start := items[0].number
	if start == 0 {
		start = 1
	}
	setOrderedAttrs(&list, start, items[0].typ)
	return list, len(strings.Join(lines[:consumed], "\n")), true
}

func setOrderedAttrs(list *Node, start int, typ string) {
	attrs := map[string]any{}
	if start != 1 {
		attrs["start"] = float64(start)
	}
	if typ != "" {
		attrs["type"] = typ
	}
	if len(attrs) > 0 {
		list.Attrs = attrs
	}
}

// buildOrdered is buildNestedStructure + OrderedList.parseMarkdown.
func (p *mdParser) buildOrdered(items []orderedItem, base int) Node {
	list := Node{Type: "orderedList", Content: []Node{}}
	for i := 0; i < len(items); {
		it := items[i]
		if it.indent != base {
			i++
			continue
		}
		para, block := splitItemContent(it.contentLines)
		var content []Node
		if main := jsTrim(strings.Join(para, "\n")); main != "" {
			content = append(content, p.paragraph(p.inlineText(main), main))
		}
		if extra := jsTrim(strings.Join(block, "\n")); extra != "" {
			content = append(content, p.blocks(extra, false)...)
		}
		j := i + 1
		for j < len(items) && items[j].indent > base {
			j++
		}
		if nested := items[i+1 : j]; len(nested) > 0 {
			minIndent := nested[0].indent
			for _, n := range nested {
				minIndent = min(minIndent, n.indent)
			}
			sub := p.buildOrdered(nested, minIndent)
			start := nested[0].number
			if start == 0 {
				start = 1
			}
			setOrderedAttrs(&sub, start, nested[0].typ)
			content = append(content, sub)
		}
		list.Content = append(list.Content, Node{Type: "listItem", Content: nonNil(content)})
		i = j
	}
	return list
}

// paragraph is Paragraph.parseMarkdown: "&nbsp;" alone is an empty line.
func (p *mdParser) paragraph(inline []Node, raw string) Node {
	if t := strings.TrimSpace(raw); (t == "&nbsp;" || t == "\u00a0") && len(inline) == 1 {
		return Node{Type: "paragraph", Content: []Node{}}
	}
	return Node{Type: "paragraph", Content: nonNil(inline)}
}

// convertBlock maps one goldmark block (no slash tokenizer matched) to nodes.
// raw is the block's source through the start of the next block.
func (p *mdParser) convertBlock(n ast.Node, sub, raw string, doc ast.Node) []Node {
	src := []byte(sub)
	switch b := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return []Node{p.paragraph(p.paragraphInlines(n, src), linesRaw(n, src))}
	case *ast.Heading:
		return []Node{{Type: "heading", Attrs: map[string]any{"level": float64(b.Level)}, Content: nonNil(p.inlines(n, src))}}
	case *ast.ThematicBreak:
		return []Node{{Type: "horizontalRule"}}
	case *ast.FencedCodeBlock:
		lang := ""
		if b.Info != nil {
			lang = jsTrim(unescapePunct(string(b.Info.Segment.Value(src))))
		}
		code := strings.TrimSuffix(linesRaw(n, src), "\n")
		var content []Node
		if code != "" {
			content = []Node{{Type: "text", Text: code}}
		}
		if lang == "mermaid" {
			return []Node{{Type: "mermaid", Content: nonNil(content)}}
		}
		var language any
		if lang != "" {
			language = lang
		}
		return []Node{{Type: "codeBlock", Attrs: map[string]any{"language": language}, Content: nonNil(content)}}
	case *ast.CodeBlock:
		code := strings.TrimRight(linesRaw(n, src), "\n")
		var content []Node
		if code != "" {
			content = []Node{{Type: "text", Text: code}}
		}
		return []Node{{Type: "codeBlock", Attrs: map[string]any{"language": nil}, Content: nonNil(content)}}
	case *ast.Blockquote:
		nested := p.nested
		p.nested = false
		content := p.blocks(stripQuote(trimBlank(raw)), true)
		p.nested = nested
		return []Node{{Type: "blockquote", Content: nonNil(content)}}
	case *ast.List:
		return p.list(b, sub, raw)
	case *east.Table:
		return []Node{p.table(b, src)}
	case *ast.HTMLBlock:
		p.lossy = true
		html := strings.TrimRightFunc(linesRaw(n, src), jsIsSpace)
		if b.HasClosure() {
			html = strings.TrimRightFunc(linesRaw(n, src)+string(b.ClosureLine.Value(src)), jsIsSpace)
		}
		if html == "" {
			return nil
		}
		return []Node{{Type: "paragraph", Content: []Node{{Type: "text", Text: html}}}}
	case *ast.LinkReferenceDefinition:
		return nil
	case *east.FootnoteList:
		p.lossy = true
		var out []Node
		for f := b.FirstChild(); f != nil; f = f.NextSibling() {
			fn, ok := f.(*east.Footnote)
			if !ok {
				continue
			}
			prefix := Node{Type: "text", Text: "[^" + string(fn.Ref) + "]: "}
			first := true
			for c := fn.FirstChild(); c != nil; c = c.NextSibling() {
				nodes := p.convertBlock(c, sub, "", doc)
				if first && len(nodes) > 0 && nodes[0].Type == "paragraph" {
					nodes[0].Content = mergeText(append([]Node{prefix}, nodes[0].Content...))
					first = false
				}
				out = append(out, nodes...)
			}
			if first {
				out = append(out, Node{Type: "paragraph", Content: []Node{prefix}})
			}
		}
		return out
	}
	p.lossy = true
	if t := jsTrim(linesRaw(n, src)); t != "" {
		return []Node{{Type: "paragraph", Content: []Node{{Type: "text", Text: t}}}}
	}
	return nil
}

// trimBlank drops the blank lines that separate a block from the next one.
func trimBlank(raw string) string {
	lines := strings.Split(raw, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

var quotePrefix = regexp.MustCompile(`(?m)^ *>[ \t]?`)

func stripQuote(raw string) string { return quotePrefix.ReplaceAllString(raw, "") }

func linesRaw(n ast.Node, src []byte) string {
	var b strings.Builder
	lines := n.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// list converts a bullet list (ordered lists go through the ordered list
// tokenizer; one it declined falls back to goldmark's items here).
func (p *mdParser) list(l *ast.List, sub, raw string) []Node {
	src := []byte(sub)
	items := blockChildren(l)
	type item struct {
		raw  string
		node Node
		task bool
	}
	var parsed []item
	listStart := lineStart(sub, nodeStart(l, src))
	listEnd := listStart + len(trimBlank(raw))
	for i, it := range items {
		start := lineStart(sub, nodeStart(it, src))
		end := listEnd
		if i+1 < len(items) {
			end = lineStart(sub, nodeStart(items[i+1], src))
		}
		itemRaw := trimBlank(sub[start:end])
		if i+1 == len(items) {
			// marked trims the end of a list's last item.
			itemRaw = strings.TrimRightFunc(itemRaw, jsIsSpace)
		}
		if !l.IsOrdered() && taskItemRe.MatchString(strings.SplitN(itemRaw, "\n", 2)[0]) {
			parsed = append(parsed, item{raw: itemRaw, task: true, node: p.mixedTaskItem(itemRaw)})
			continue
		}
		body := listItemBody(itemRaw)
		nested := p.nested
		p.nested = true
		content := p.blocks(body, !l.IsTight)
		p.nested = nested
		if len(content) == 0 {
			content = []Node{{Type: "paragraph", Content: []Node{}}}
		}
		parsed = append(parsed, item{raw: itemRaw, node: Node{Type: "listItem", Content: content}})
	}
	listType := "bulletList"
	if l.IsOrdered() {
		listType = "orderedList"
	}
	var out []Node
	for i := 0; i < len(parsed); {
		j := i
		for j < len(parsed) && parsed[j].task == parsed[i].task {
			j++
		}
		group := Node{Type: listType, Content: []Node{}}
		if parsed[i].task {
			group.Type = "taskList"
		} else if l.IsOrdered() {
			setOrderedAttrs(&group, max(l.Start, 1), "")
		}
		for _, it := range parsed[i:j] {
			group.Content = append(group.Content, it.node)
		}
		out = append(out, group)
		i = j
	}
	return out
}

// mixedTaskItem is parseListToken's task item: first line is the item,
// the rest (dedented to its least indented line) is lexed as blocks.
func (p *mdParser) mixedTaskItem(raw string) Node {
	lines := strings.Split(raw, "\n")
	m := taskItemRe.FindStringSubmatch(lines[0])
	main := ""
	if m != nil {
		main = m[4]
	}
	content := []Node{{Type: "paragraph", Content: p.inlineText(main)}}
	if rest := lines[1:]; len(rest) > 0 && jsTrim(strings.Join(rest, "\n")) != "" {
		minIndent := -1
		for _, line := range rest {
			if jsTrim(line) == "" {
				continue
			}
			w := len(line) - len(strings.TrimLeftFunc(line, jsIsSpace))
			if minIndent < 0 || w < minIndent {
				minIndent = w
			}
		}
		dedented := make([]string, len(rest))
		for i, line := range rest {
			if jsTrim(line) != "" {
				dedented[i] = line[minIndent:]
			}
		}
		if nested := jsTrim(strings.Join(dedented, "\n")); nested != "" {
			content = append(content, p.blocks(nested+"\n", false)...)
		}
	}
	checked := m != nil && strings.EqualFold(m[3], "x")
	return Node{Type: "taskItem", Attrs: map[string]any{"checked": checked}, Content: content}
}

// listItemBody strips a list item's marker and its content indentation.
func listItemBody(raw string) string {
	lines := strings.Split(raw, "\n")
	first := lines[0]
	i := 0
	for i < len(first) && first[i] == ' ' {
		i++
	}
	for i < len(first) && (first[i] >= '0' && first[i] <= '9') {
		i++
	}
	if i < len(first) {
		i++ // the marker character (-, +, *, . or ))
	}
	spaces := 0
	for i+spaces < len(first) && first[i+spaces] == ' ' {
		spaces++
	}
	switch {
	case i+spaces == len(first):
		spaces = min(spaces, 1)
	case spaces > 4:
		spaces = 1
	}
	indent := i + spaces
	out := make([]string, len(lines))
	out[0] = jsSlice(first, indent)
	for k, line := range lines[1:] {
		w := 0
		for w < len(line) && w < indent && line[w] == ' ' {
			w++
		}
		out[k+1] = line[w:]
	}
	return strings.Join(out, "\n")
}

func (p *mdParser) table(t *east.Table, src []byte) Node {
	table := Node{Type: "table", Content: []Node{}}
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		cellType := "tableCell"
		if _, ok := r.(*east.TableHeader); ok {
			cellType = "tableHeader"
		}
		row := Node{Type: "tableRow", Content: []Node{}}
		col := 0
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			var attrs map[string]any
			if col < len(t.Alignments) {
				switch t.Alignments[col] {
				case east.AlignLeft:
					attrs = map[string]any{"align": "left"}
				case east.AlignRight:
					attrs = map[string]any{"align": "right"}
				case east.AlignCenter:
					attrs = map[string]any{"align": "center"}
				}
			}
			row.Content = append(row.Content, Node{
				Type:    cellType,
				Attrs:   attrs,
				Content: []Node{{Type: "paragraph", Content: nonNil(p.inlines(c, src))}},
			})
			col++
		}
		table.Content = append(table.Content, row)
	}
	return table
}

// inlineText is marked's inlineTokens on a bare string.
func (p *mdParser) inlineText(s string) []Node {
	lead := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
	trail := ""
	if strings.TrimLeft(s, " \t") != "" {
		trail = s[len(strings.TrimRight(s, " \t")):]
	}
	core := s[len(lead) : len(s)-len(trail)]
	var out []Node
	if lead != "" {
		out = append(out, Node{Type: "text", Text: lead})
	}
	if core != "" {
		src := []byte(core)
		doc := p.parse(inlineGM, src)
		for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
			out = append(out, p.inlines(c, src)...)
		}
	}
	if trail != "" {
		out = append(out, Node{Type: "text", Text: trail})
	}
	return mergeText(out)
}

// paragraphInlines is a paragraph's inline content as marked lexes it:
// goldmark trims every line's leading whitespace and the last line's
// trailing whitespace, marked keeps them as text (softBreak and hardBreak
// restore the continuation lines' indentation).
func (p *mdParser) paragraphInlines(n ast.Node, src []byte) []Node {
	lines := n.Lines()
	if lines.Len() == 0 {
		return p.inlines(n, src)
	}
	first, last := lines.At(0), lines.At(lines.Len()-1)
	lead := first.Start
	for lead > 0 && isBlankByte(src[lead-1]) {
		lead--
	}
	if lead > 0 && src[lead-1] != '\n' {
		lead = first.Start // not at a line start (a footnote body)
	}
	trail := last.Stop
	for trail < len(src) && isBlankByte(src[trail]) {
		trail++
	}
	var out []Node
	if lead < first.Start {
		out = append(out, Node{Type: "text", Text: string(src[lead:first.Start])})
	}
	out = append(out, p.inlines(n, src)...)
	if trail > last.Stop {
		out = append(out, Node{Type: "text", Text: string(src[last.Stop:trail])})
	}
	return mergeText(out)
}

func isBlankByte(c byte) bool { return c == ' ' || c == '\t' }

// inlines converts a block's inline children.
func (p *mdParser) inlines(parent ast.Node, src []byte) []Node {
	var out []Node
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, p.inline(c, src)...)
	}
	if p.inLink {
		return mergeText(out)
	}
	return mergeText(linkify(mergeText(out)))
}

func (p *mdParser) inline(n ast.Node, src []byte) []Node {
	switch x := n.(type) {
	case *ast.Text:
		var out []Node
		if t := unescapeText(string(x.Segment.Value(src))); t != "" {
			out = append(out, Node{Type: "text", Text: t})
		}
		switch {
		case x.HardLineBreak():
			out = append(out, Node{Type: "hardBreak"})
			if indent := nextLineIndent(x, src); indent != "" {
				out = append(out, Node{Type: "text", Text: indent})
			}
		case x.SoftLineBreak():
			out = append(out, Node{Type: "text", Text: softBreak(x, src) + nextLineIndent(x, src)})
		}
		return out
	case *ast.String:
		return []Node{{Type: "text", Text: string(x.Value)}}
	case *ast.CodeSpan:
		var b strings.Builder
		for c := x.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			} else if s, ok := c.(*ast.String); ok {
				b.Write(s.Value)
			}
		}
		code := strings.ReplaceAll(b.String(), "\n", " ")
		if code == "" {
			return nil
		}
		return []Node{{Type: "text", Text: code, Marks: []Mark{{Type: "code"}}}}
	case *ast.Emphasis:
		mark := "italic"
		if x.Level >= 2 {
			mark = "bold"
		}
		return applyMark(p.inlines(x, src), Mark{Type: mark})
	case *east.Strikethrough:
		if pos := x.Pos(); pos >= 0 && pos+1 < len(src) && src[pos+1] != '~' {
			p.lossy = true
		}
		return applyMark(p.inlines(x, src), Mark{Type: "strike"})
	case *ast.Link:
		var title any
		if t := unescapeText(string(x.Title)); t != "" {
			title = t
		}
		inLink := p.inLink
		p.inLink = true
		label := p.inlines(x, src)
		p.inLink = inLink
		return applyMark(label, Mark{Type: "link", Attrs: map[string]any{
			"href":  unescapePunct(string(x.Destination)),
			"title": title,
		}})
	case *ast.AutoLink:
		label := string(x.Label(src))
		href := string(x.URL(src))
		if x.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(href), "mailto:") {
			href = "mailto:" + href
		}
		return []Node{{Type: "text", Text: decodeEntities(label), Marks: []Mark{{Type: "link", Attrs: map[string]any{"href": href, "title": nil}}}}}
	case *ast.Image:
		// Image.parseMarkdown for an inline image: its text, the image is lost.
		p.lossy = true
		alt := plainText(x, src)
		if alt == "" {
			alt = string(x.Destination)
		}
		return []Node{{Type: "text", Text: alt}}
	case *ast.RawHTML:
		var b strings.Builder
		for i := range x.Segments.Len() {
			seg := x.Segments.At(i)
			b.Write(seg.Value(src))
		}
		html := b.String()
		if inlineMarker.MatchString(html) {
			return nil
		}
		p.lossy = true
		if t := strings.TrimRightFunc(html, jsIsSpace); t != "" {
			return []Node{{Type: "text", Text: t}}
		}
		return nil
	case *east.TaskCheckBox:
		return nil
	case *east.FootnoteLink:
		p.lossy = true
		return []Node{{Type: "text", Text: "[^" + footnoteRef(x) + "]"}}
	case *east.FootnoteBacklink:
		return nil
	}
	p.lossy = true
	return p.inlines(n, src)
}

func footnoteRef(l *east.FootnoteLink) string {
	var doc ast.Node = l
	for doc.Parent() != nil {
		doc = doc.Parent()
	}
	ref := ""
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if f, ok := n.(*east.Footnote); ok && entering && f.Index == l.Index {
			ref = string(f.Ref)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return ref
}

// softBreak is the source between a line's text and the next line: the
// trailing whitespace marked keeps plus the newline.
func softBreak(t *ast.Text, src []byte) string {
	end := t.Segment.Stop
	i := end
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	if i < len(src) && src[i] == '\n' {
		return string(src[end : i+1])
	}
	return "\n"
}

// nextLineIndent is the leading whitespace of the line after t's line
// break, which goldmark trims and marked keeps.
func nextLineIndent(t *ast.Text, src []byte) string {
	i := t.Segment.Stop
	for i < len(src) && src[i] != '\n' {
		i++
	}
	j := i + 1
	for j < len(src) && isBlankByte(src[j]) {
		j++
	}
	if j > len(src) {
		return ""
	}
	return string(src[i+1 : j])
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := c.(*ast.Text); ok && entering {
			b.WriteString(unescapeText(string(t.Segment.Value(src))))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func applyMark(content []Node, m Mark) []Node {
	for i := range content {
		if content[i].Type == "text" {
			marks := make([]Mark, 0, len(content[i].Marks)+1)
			marks = append(marks, content[i].Marks...)
			content[i].Marks = append(marks, m)
		}
	}
	return content
}

// mergeText joins adjacent text nodes with equal mark sets and drops empty ones.
func mergeText(nodes []Node) []Node {
	out := nodes[:0:0]
	for _, n := range nodes {
		if n.Type == "text" && n.Text == "" {
			continue
		}
		if k := len(out) - 1; k >= 0 && n.Type == "text" && out[k].Type == "text" && marksEqual(out[k].Marks, n.Marks) {
			out[k].Text += n.Text
			continue
		}
		out = append(out, n)
	}
	return out
}

// marksEqual is order-insensitive, as @tiptap/core's.
func marksEqual(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, ma := range a {
		found := false
		for j, mb := range b {
			if !used[j] && ma.Type == mb.Type && attrsEqual(ma.Attrs, mb.Attrs) {
				used[j], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// unescapeText resolves backslash escapes and the four entities
// decodeHtmlEntities knows; escaped characters are never entity-decoded.
func unescapeText(s string) string {
	if !strings.ContainsAny(s, "\\&") {
		return s
	}
	var b, run strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			b.WriteString(decodeEntities(run.String()))
			run.Reset()
			b.WriteByte(s[i+1])
			i++
			continue
		}
		run.WriteByte(s[i])
	}
	b.WriteString(decodeEntities(run.String()))
	return b.String()
}

func unescapePunct(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

var entityDecoder = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", "\"")

func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return strings.ReplaceAll(entityDecoder.Replace(s), "&amp;", "&")
}
