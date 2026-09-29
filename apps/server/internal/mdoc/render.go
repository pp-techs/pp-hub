package mdoc

import (
	"sort"
	"strconv"
	"strings"
)

// Render serializes doc to markdown byte-identical to slash-editor's
// serializeMarkdown for the v1 block set. It omits ids and a trailing empty
// paragraph and prepends frontmatter.
//
// It is a port of @tiptap/markdown's MarkdownManager.serialize (mark
// boundary tracking included) and of the renderMarkdown hooks of every node
// and mark in the v1 kit; the golden fixtures pin it to the JS original.
func Render(doc Node, frontmatter []byte) []byte {
	if n := len(doc.Content); n > 0 && isEmptyParagraph(doc.Content[n-1]) {
		trimmed := doc
		trimmed.Content = doc.Content[:n-1]
		doc = trimmed
	}
	doc = pruneExcluded(doc)
	out := renderNode(&doc, &doc, 0)
	if isEmptyOutput(out) {
		out = ""
	}
	res := make([]byte, 0, len(frontmatter)+len(out))
	res = append(res, frontmatter...)
	return append(res, out...)
}

func isEmptyParagraph(n Node) bool { return n.Type == "paragraph" && len(n.Content) == 0 }

func isEmptyOutput(md string) bool {
	if jsTrim(md) == "" {
		return true
	}
	md = strings.ReplaceAll(md, "&nbsp;", "")
	md = strings.ReplaceAll(md, "\u00a0", "")
	return jsTrim(md) == ""
}

// excludedFromMarkdown mirrors the media nodes' excludeFromMarkdown: a
// placeholder or an upload that has not landed has no URL to write.
func excludedFromMarkdown(n *Node) bool {
	if n.Type != "image" && n.Type != "file" {
		return false
	}
	if !truthy(n.Attrs["src"]) {
		return true
	}
	status, ok := n.Attrs["status"]
	if !ok || status == nil {
		return false
	}
	return status != "ready"
}

// pruneExcluded is SlashMarkdownManager's pruneExcluded: drop excluded
// nodes; a container left empty gets one empty paragraph.
func pruneExcluded(n Node) Node {
	if len(n.Content) == 0 {
		return n
	}
	changed := false
	content := make([]Node, 0, len(n.Content))
	for i := range n.Content {
		child := &n.Content[i]
		if excludedFromMarkdown(child) {
			changed = true
			continue
		}
		pruned := pruneExcluded(*child)
		if !changed && !sameNode(&pruned, child) {
			changed = true
		}
		content = append(content, pruned)
	}
	if !changed {
		return n
	}
	if len(content) == 0 {
		content = []Node{{Type: "paragraph", Content: []Node{}}}
	}
	n.Content = content
	return n
}

// sameNode reports whether pruneExcluded returned child unchanged (it only
// ever rebuilds the content slice).
func sameNode(a, b *Node) bool {
	return len(a.Content) == len(b.Content) && (len(a.Content) == 0 || &a.Content[0] == &b.Content[0])
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return true
}

// renderNode is renderNodeToMarkdown: parent is the node whose handler is
// rendering node, index node's position in parent.Content (used to find the
// previous sibling).
func renderNode(n, parent *Node, index int) string {
	if n.Type == "text" {
		return encodeText(n.Text, n, parent)
	}
	var prev *Node
	if parent != nil && index > 0 && index-1 < len(parent.Content) {
		prev = &parent.Content[index-1]
	}
	h := helpers{node: n}
	switch n.Type {
	case "doc":
		if n.Content == nil {
			return ""
		}
		return h.children(n.Content, "\n\n")
	case "paragraph":
		if len(n.Content) == 0 {
			if prev != nil && prev.Type == "paragraph" && len(prev.Content) == 0 {
				return "&nbsp;"
			}
			return ""
		}
		return h.children(n.Content, "")
	case "heading":
		level := 1
		if truthy(n.Attrs["level"]) {
			level = jsParseInt(n.Attrs["level"])
		}
		if n.Content == nil {
			return ""
		}
		return strings.Repeat("#", level) + " " + h.children(n.Content, "")
	case "blockquote":
		if n.Content == nil {
			return ""
		}
		parts := make([]string, len(n.Content))
		for i := range n.Content {
			lines := strings.Split(h.child(&n.Content[i], i), "\n")
			for j, line := range lines {
				if jsTrim(line) == "" {
					lines[j] = ">"
				} else {
					lines[j] = "> " + line
				}
			}
			parts[i] = strings.Join(lines, "\n")
		}
		return strings.Join(parts, "\n>\n")
	case "bulletList", "orderedList", "taskList":
		if n.Content == nil {
			return ""
		}
		return h.children(n.Content, "\n")
	case "listItem":
		prefix := "- "
		align := false
		if parent != nil && parent.Type == "orderedList" {
			start := 1
			if truthy(parent.Attrs["start"]) {
				start = jsParseInt(parent.Attrs["start"])
			}
			prefix = listMarker(parent.Attrs["type"], start-1+index)
			align = true
		}
		return h.nested(prefix, align)
	case "taskItem":
		prefix := "- [ ] "
		if truthy(n.Attrs["checked"]) {
			prefix = "- [x] "
		}
		return h.nested(prefix, false)
	case "codeBlock":
		lang := ""
		if truthy(n.Attrs["language"]) {
			lang = attrString(n.Attrs["language"])
		}
		if n.Content == nil {
			return "```" + lang + "\n\n```"
		}
		return "```" + lang + "\n" + h.children(n.Content, "") + "\n```"
	case "mermaid":
		return "```mermaid\n" + h.children(nonNil(n.Content), "") + "\n```"
	case "horizontalRule":
		return "---"
	case "hardBreak":
		return "  \n"
	case "table":
		return renderTable(n, h)
	case "callout":
		icon := "💡"
		if v, ok := n.Attrs["icon"]; ok && v != nil {
			icon = attrString(v)
		}
		alert, known := alertByIcon[icon]
		body := h.children(nonNil(n.Content), "\n\n")
		if !known {
			alert = "NOTE"
		}
		lines := strings.Split("[!"+alert+"]\n"+body, "\n")
		for i, line := range lines {
			if line == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + line
			}
		}
		quoted := strings.Join(lines, "\n")
		if known {
			return quoted
		}
		return renderMarker("callout", orderedAttrs{{"icon", icon}}) + "\n" + quoted
	case "details":
		var summary, content *Node
		if len(n.Content) > 0 {
			summary = &n.Content[0]
		}
		if len(n.Content) > 1 {
			content = &n.Content[1]
		}
		level := toggleLevel(n.Attrs["level"])
		var title, body string
		if summary != nil {
			title = h.children(nonNil(summary.Content), "")
		} else {
			title = h.children([]Node{}, "")
		}
		if content != nil {
			body = h.children(nonNil(content.Content), "\n\n")
		}
		open := "<details>"
		if truthy(n.Attrs["open"]) {
			open = "<details open>"
		}
		if level != 0 {
			l := strconv.Itoa(level)
			title = "<h" + l + ">" + title + "</h" + l + ">"
		}
		lines := []string{open, "<summary>" + title + "</summary>"}
		if body != "" {
			lines = append(lines, "", body, "")
		}
		return strings.Join(append(lines, "</details>"), "\n")
	case "columns":
		cols := make([]string, len(n.Content))
		for i := range n.Content {
			cols[i] = h.children(nonNil(n.Content[i].Content), "\n\n")
		}
		return strings.Join([]string{
			renderMarker("columns", nil),
			strings.Join(cols, "\n\n"+renderMarker("column", nil)+"\n\n"),
			"<!-- /slash:columns -->",
		}, "\n\n")
	case "image":
		return renderMedia(n, "image", "alt", []string{"width"}, true)
	case "file":
		return renderMedia(n, "file", "name", []string{"size", "mime"}, false)
	}
	return ""
}

func nonNil(c []Node) []Node {
	if c == nil {
		return []Node{}
	}
	return c
}

// helpers is the `h` a renderMarkdown hook receives.
type helpers struct{ node *Node }

// children is h.renderChildren(nodes, separator).
func (h helpers) children(nodes []Node, sep string) string {
	return renderInline(nodes, h.node, sep)
}

// child is h.renderChild(node, index).
func (h helpers) child(n *Node, index int) string { return renderNode(n, h.node, index) }

// nested is renderNestedMarkdownContent.
func (h helpers) nested(prefix string, alignToPrefix bool) string {
	n := h.node
	if n.Content == nil {
		return ""
	}
	if len(n.Content) == 0 {
		return prefix
	}
	out := prefix + h.children(n.Content[:1], "")
	indent := "  "
	if alignToPrefix && columnWidth(prefix) > 2 {
		indent = strings.Repeat(" ", columnWidth(prefix))
	}
	for i := 1; i < len(n.Content); i++ {
		child := &n.Content[i]
		lines := strings.Split(h.child(child, i), "\n")
		for j, line := range lines {
			lines[j] = indent + line
		}
		sep := "\n"
		if child.Type == "paragraph" {
			sep = "\n\n"
		}
		out += sep + strings.Join(lines, "\n")
	}
	return out
}

func columnWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 4 - w%4
		} else {
			w++
		}
	}
	return w
}

var alertByIcon = map[string]string{
	"💡":  "TIP",
	"ℹ️": "NOTE",
	"❗":  "IMPORTANT",
	"⚠️": "WARNING",
	"⛔":  "CAUTION",
}

func toggleLevel(v any) int {
	f, ok := v.(float64)
	if !ok {
		if i, isInt := v.(int); isInt {
			f, ok = float64(i), true
		}
	}
	if ok && (f == 1 || f == 2 || f == 3) {
		return int(f)
	}
	return 0
}

func jsParseInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		digits := strings.TrimLeftFunc(x, jsIsSpace)
		end := 0
		for end < len(digits) && (digits[end] >= '0' && digits[end] <= '9' || end == 0 && (digits[end] == '-' || digits[end] == '+')) {
			end++
		}
		n, _ := strconv.Atoi(digits[:end])
		return n
	}
	return 0
}

func attrString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case float64:
		return jsNumber(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return jsJSON(v)
}

// listMarker is getListMarker(type, index, ". ").
func listMarker(typ any, index int) string {
	pos := index + 1
	t, _ := typ.(string)
	switch t {
	case "a":
		return alphaNumeral(pos) + ". "
	case "A":
		return strings.ToUpper(alphaNumeral(pos)) + ". "
	case "i":
		return romanNumeral(pos) + ". "
	case "I":
		return strings.ToUpper(romanNumeral(pos)) + ". "
	}
	return strconv.Itoa(pos) + ". "
}

var romanNumerals = []struct {
	value   int
	numeral string
}{
	{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"},
	{50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"},
}

func romanNumeral(n int) string {
	var b strings.Builder
	for _, rn := range romanNumerals {
		for n >= rn.value {
			b.WriteString(rn.numeral)
			n -= rn.value
		}
	}
	return b.String()
}

func alphaNumeral(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	if n <= 26 {
		if n < 1 {
			return "undefined"
		}
		return letters[n-1 : n]
	}
	first := (n-1)/26 - 1
	second := (n - 1) % 26
	if first < 0 {
		return letters[second : second+1]
	}
	if first >= 26 {
		return "undefined" + letters[second:second+1]
	}
	return letters[first:first+1] + letters[second:second+1]
}

// renderMarker is `<!-- slash:<type> {json} -->` with nullish attrs dropped.
func renderMarker(typ string, attrs orderedAttrs) string {
	defined := make(orderedAttrs, 0, len(attrs))
	for _, kv := range attrs {
		if kv.value != nil {
			defined = append(defined, kv)
		}
	}
	if len(defined) == 0 {
		return "<!-- slash:" + typ + " -->"
	}
	payload := strings.ReplaceAll(jsJSON(defined), "--", `\u002d\u002d`)
	return "<!-- slash:" + typ + " " + payload + " -->"
}

// renderMedia is mediaMarkdown's renderMarkdown.
func renderMedia(n *Node, typ, labelAttr string, markerKeys []string, image bool) string {
	href := attrString(n.Attrs["src"])
	carried := orderedAttrs{}
	for _, k := range markerKeys {
		if v, ok := n.Attrs[k]; ok {
			carried = append(carried, attrKV{k, v})
		}
	}
	allNull := true
	for _, kv := range carried {
		if kv.value != nil {
			allNull = false
		}
	}
	out := ""
	if !image || !allNull {
		out = renderMarker(typ, carried) + "\n"
	}
	label := ""
	if truthy(n.Attrs[labelAttr]) {
		label = attrString(n.Attrs[labelAttr])
	} else if !image {
		label = href
	}
	return out + renderLinkLine(label, href, image)
}

func renderLinkLine(label, href string, image bool) string {
	var text strings.Builder
	for _, r := range label {
		if r == '\\' || r == '[' || r == ']' {
			text.WriteByte('\\')
		}
		text.WriteRune(r)
	}
	dest := href
	if strings.ContainsFunc(href, func(r rune) bool { return jsIsSpace(r) || strings.ContainsRune("()<>", r) }) {
		dest = "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(href) + ">"
	}
	bang := ""
	if image {
		bang = "!"
	}
	return bang + "[" + text.String() + "](" + dest + ")"
}

// renderTable is @tiptap/extension-table's renderTableToMarkdown.
func renderTable(n *Node, h helpers) string {
	const cellSep = "\x1f"
	if len(n.Content) == 0 {
		return ""
	}
	type cell struct {
		text     string
		isHeader bool
		align    string
	}
	var rows [][]cell
	for ri := range n.Content {
		row := &n.Content[ri]
		var cells []cell
		for ci := range row.Content {
			c := &row.Content[ci]
			var raw string
			if len(c.Content) > 1 {
				parts := make([]string, len(c.Content))
				for k := range c.Content {
					child := &c.Content[k]
					if child.Content != nil {
						parts[k] = renderInline(child.Content, h.node, "")
					} else {
						parts[k] = renderNode(child, h.node, k)
					}
				}
				raw = strings.Join(parts, cellSep)
			} else if c.Content != nil {
				raw = h.children(c.Content, "")
			}
			text := strings.ReplaceAll(raw, cellSep, "\n")
			text = jsCollapseSpace(cellBreaks(text))
			align, _ := c.Attrs["align"].(string)
			if align != "left" && align != "right" && align != "center" {
				align = ""
			}
			cells = append(cells, cell{text, c.Type == "tableHeader", align})
		}
		rows = append(rows, cells)
	}
	columns := 0
	for _, r := range rows {
		columns = max(columns, len(r))
	}
	if columns == 0 {
		return ""
	}
	widths := make([]int, columns)
	for _, r := range rows {
		for i := range columns {
			if i < len(r) {
				widths[i] = max(widths[i], jsLen(r[i].text))
			}
			widths[i] = max(widths[i], 3)
		}
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", max(0, w-jsLen(s))) }
	textAt := func(r []cell, i int) string {
		if i < len(r) {
			return r[i].text
		}
		return ""
	}
	hasHeader := false
	for _, c := range rows[0] {
		hasHeader = hasHeader || c.isHeader
	}
	aligns := make([]string, columns)
	for _, r := range rows {
		for i := range columns {
			if aligns[i] == "" && i < len(r) && r[i].align != "" {
				aligns[i] = r[i].align
			}
		}
	}
	var out strings.Builder
	out.WriteString("\n")
	header := make([]string, columns)
	for i := range columns {
		t := ""
		if hasHeader {
			t = textAt(rows[0], i)
		}
		header[i] = pad(t, widths[i])
	}
	out.WriteString("| " + strings.Join(header, " | ") + " |\n")
	seps := make([]string, columns)
	for i, w := range widths {
		dashes := strings.Repeat("-", max(3, w))
		switch aligns[i] {
		case "left":
			seps[i] = ":" + dashes
		case "right":
			seps[i] = dashes + ":"
		case "center":
			seps[i] = ":" + dashes + ":"
		default:
			seps[i] = dashes
		}
	}
	out.WriteString("| " + strings.Join(seps, " | ") + " |\n")
	body := rows
	if hasHeader {
		body = rows[1:]
	}
	for _, r := range body {
		cells := make([]string, columns)
		for i := range columns {
			cells[i] = pad(textAt(r, i), widths[i])
		}
		out.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return out.String()
}

// cellBreaks is `.replace(/[ \t]*\r?\n[ \t]*/g, "<br>")`.
func cellBreaks(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	var b strings.Builder
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			line = strings.TrimLeft(line, " \t")
		}
		if i < len(lines)-1 {
			line = strings.TrimSuffix(line, "\r")
			line = strings.TrimRight(line, " \t")
			b.WriteString(line)
			b.WriteString("<br>")
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// encodeText is encodeTextForMarkdown.
func encodeText(text string, n, parent *Node) string {
	if parent != nil && isCodeType(parent.Type) {
		return text
	}
	for _, m := range n.Marks {
		if isCodeType(m.Type) {
			return text
		}
	}
	var b strings.Builder
	for _, r := range text {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\\', '`', '*', '_', '[', ']', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// markMap is a JS Map<string, Mark>: insertion-ordered, re-set keeps position.
type markMap struct {
	keys []string
	vals map[string]*Mark
}

func newMarkMap() *markMap { return &markMap{vals: map[string]*Mark{}} }

func marksToMap(marks []Mark) *markMap {
	m := newMarkMap()
	for i := range marks {
		m.set(marks[i].Type, &marks[i])
	}
	return m
}

func (m *markMap) has(k string) bool  { _, ok := m.vals[k]; return ok }
func (m *markMap) get(k string) *Mark { return m.vals[k] }
func (m *markMap) size() int          { return len(m.keys) }
func (m *markMap) set(k string, v *Mark) {
	if !m.has(k) {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *markMap) delete(k string) {
	if !m.has(k) {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *markMap) clear() {
	m.keys = nil
	m.vals = map[string]*Mark{}
}

func (m *markMap) without(drop map[string]bool) *markMap {
	out := newMarkMap()
	for _, k := range m.keys {
		if !drop[k] {
			out.set(k, m.vals[k])
		}
	}
	return out
}

// attrsEqual is @tiptap/core's attrsEqual: a nil map is `undefined`.
func attrsEqual(a, b map[string]any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil || len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !jsSame(va, vb) {
			return false
		}
	}
	return true
}

// jsSame is Object.is for JSON scalars; objects compare by identity in JS,
// which two decoded values never share.
func jsSame(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case int:
		y, ok := b.(int)
		return ok && x == y
	}
	return false
}

func findMark(marks []Mark, typ string, attrs map[string]any) bool {
	for i := range marks {
		if marks[i].Type == typ && attrsEqual(marks[i].Attrs, attrs) {
			return true
		}
	}
	return false
}

type markToOpen struct {
	typ  string
	mark *Mark
}

func findMarksToOpen(active, current *markMap) []markToOpen {
	var out []markToOpen
	for _, k := range current.keys {
		m := current.get(k)
		if am := active.get(k); am == nil || !attrsEqual(am.Attrs, m.Attrs) {
			out = append(out, markToOpen{k, m})
		}
	}
	return out
}

func marksToOpenForSerialization(active, current *markMap, next *Node) []markToOpen {
	open := findMarksToOpen(active, current)
	if len(open) <= 1 {
		return open
	}
	var nextMarks []Mark
	if next != nil {
		nextMarks = next.Marks
	}
	var ending, continuing []markToOpen
	for _, m := range open {
		if findMark(nextMarks, m.typ, m.mark.Attrs) {
			continuing = append(continuing, m)
		} else {
			ending = append(ending, m)
		}
	}
	innerFirst := func(s []markToOpen) {
		sort.SliceStable(s, func(i, j int) bool {
			ri, rj := markRank(s[i].typ), markRank(s[j].typ)
			if ri != rj {
				return ri > rj
			}
			return s[i].typ < s[j].typ
		})
	}
	innerFirst(ending)
	innerFirst(continuing)
	return append(ending, continuing...)
}

func findMarksToClose(current *markMap, next *Node) []string {
	var out []string
	for _, k := range current.keys {
		if next == nil || !findMark(next.Marks, k, current.get(k).Attrs) {
			out = append(out, k)
		}
	}
	return out
}

func markSetsEqual(a, b *markMap) bool {
	if a.size() != b.size() {
		return false
	}
	for _, k := range a.keys {
		o := b.get(k)
		if o == nil || !attrsEqual(a.get(k).Attrs, o.Attrs) {
			return false
		}
	}
	return true
}

func findMarksToCloseAtEnd(active, current *markMap, next *Node) []string {
	isLast := next == nil
	nextNoMarks := next != nil && len(next.Marks) == 0
	nextDiff := next != nil && next.Marks != nil && !markSetsEqual(current, marksToMap(next.Marks))
	var out []string
	if isLast || nextNoMarks || nextDiff {
		if next != nil && next.Marks != nil {
			for i := len(active.keys) - 1; i >= 0; i-- {
				k := active.keys[i]
				if !findMark(next.Marks, k, active.get(k).Attrs) {
					out = append(out, k)
				}
			}
		} else {
			for i := len(active.keys) - 1; i >= 0; i-- {
				out = append(out, active.keys[i])
			}
		}
	}
	return out
}

const (
	openMarkdown = "markdown"
	openHTML     = "html"
)

func htmlReopen(typ string) (open, closing string, ok bool) {
	switch typ {
	case "bold":
		return "<strong>", "</strong>", true
	case "italic":
		return "<em>", "</em>", true
	}
	return "", "", false
}

func markOpening(typ string, m *Mark, mode string) string {
	if mode == openHTML {
		open, _, _ := htmlReopen(typ)
		return open
	}
	switch typ {
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	case "code":
		return "`"
	case "link":
		return "["
	}
	return ""
}

func markClosing(typ string, m *Mark, mode string) string {
	if mode == openHTML {
		_, closing, _ := htmlReopen(typ)
		return closing
	}
	switch typ {
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "strike":
		return "~~"
	case "code":
		return "`"
	case "link":
		var attrs map[string]any
		if m != nil {
			attrs = m.Attrs
		}
		href := ""
		if v, ok := attrs["href"]; ok && v != nil {
			href = attrString(v)
		}
		title := ""
		if v, ok := attrs["title"]; ok && v != nil {
			title = attrString(v)
		}
		if title != "" {
			return "](" + href + ` "` + title + `")`
		}
		return "](" + href + ")"
	}
	return ""
}

// renderInline is renderNodesWithMarkBoundaries.
func renderInline(nodes []Node, parent *Node, sep string) string {
	result := make([]string, 0, len(nodes))
	active := newMarkMap()
	reopenHTML := map[string]bool{}
	modes := map[string]string{}
	modeOf := func(k string) string {
		if m, ok := modes[k]; ok {
			return m
		}
		return openMarkdown
	}
	for i := range nodes {
		node := &nodes[i]
		var next *Node
		if i < len(nodes)-1 {
			next = &nodes[i+1]
		}
		if node.Type == "" {
			continue
		}
		if node.Type != "text" {
			nodeMarks := map[string]bool{}
			for _, m := range node.Marks {
				nodeMarks[m.Type] = true
			}
			reopen := newMarkMap()
			reopenModes := map[string]string{}
			for _, k := range active.keys {
				if nodeMarks[k] {
					reopen.set(k, active.get(k))
					reopenModes[k] = modeOf(k)
				}
			}
			before := ""
			for j := len(active.keys) - 1; j >= 0; j-- {
				k := active.keys[j]
				before = markClosing(k, active.get(k), modeOf(k)) + before
			}
			active.clear()
			clear(modes)
			content := renderNode(node, parent, i)
			after := ""
			if node.Type != "hardBreak" {
				for _, k := range reopen.keys {
					mode := reopenModes[k]
					if mode == "" {
						mode = openMarkdown
					}
					modes[k] = mode
					after += markOpening(k, reopen.get(k), mode)
					active.set(k, reopen.get(k))
				}
			}
			result = append(result, before+content+after)
			continue
		}

		text := encodeText(node.Text, node, parent)
		current := marksToMap(node.Marks)
		toOpen := marksToOpenForSerialization(active, current, next)
		toClose := findMarksToClose(current, next)
		if len(text) > 0 && jsTrim(text) == "" && current.size() > 0 {
			transient := map[string]bool{}
			for _, k := range toClose {
				if !active.has(k) {
					transient[k] = true
				}
			}
			if len(transient) > 0 {
				current = current.without(transient)
				toOpen = marksToOpenForSerialization(active, current, next)
				toClose = findMarksToClose(current, next)
			}
		}
		var activeClosingHere []string
		for _, k := range toClose {
			if active.has(k) {
				activeClosingHere = append(activeClosingHere, k)
			}
		}
		crossed := len(activeClosingHere) > 0 && len(toOpen) > 0
		middleWS := ""
		if len(toClose) > 0 && !crossed {
			middleWS = jsTrailingSpace(text)
			text = text[:len(text)-len(middleWS)]
		}
		if !crossed {
			for j := len(toClose) - 1; j >= 0; j-- {
				k := toClose[j]
				if !active.has(k) {
					continue
				}
				text += markClosing(k, current.get(k), modeOf(k))
				active.delete(k)
				delete(modes, k)
			}
		}
		leadingWS := ""
		if len(toOpen) > 0 {
			leadingWS = jsLeadingSpace(text)
			text = text[len(leadingWS):]
		}
		for _, o := range toOpen {
			mode := openMarkdown
			if reopenHTML[o.typ] {
				mode = openHTML
			}
			text = markOpening(o.typ, o.mark, mode) + text
			modes[o.typ] = mode
			delete(reopenHTML, o.typ)
		}
		if !crossed {
			for j := len(toOpen) - 1; j >= 0; j-- {
				active.set(toOpen[j].typ, toOpen[j].mark)
			}
		}
		text = leadingWS + text
		var closeAtEnd []string
		if crossed {
			nextTypes := map[string]bool{}
			if next != nil {
				for _, m := range next.Marks {
					nextTypes[m.Type] = true
				}
			}
			for _, o := range toOpen {
				if _, _, ok := htmlReopen(o.typ); ok && nextTypes[o.typ] {
					reopenHTML[o.typ] = true
				}
			}
			index := map[string]int{}
			for j, k := range active.keys {
				index[k] = j
			}
			lifo := append([]string(nil), activeClosingHere...)
			sort.SliceStable(lifo, func(a, b int) bool { return index[lifo[a]] > index[lifo[b]] })
			for _, o := range toOpen {
				closeAtEnd = append(closeAtEnd, o.typ)
			}
			closeAtEnd = append(closeAtEnd, lifo...)
		} else {
			closeAtEnd = findMarksToCloseAtEnd(active, current, next)
		}
		trailingWS := ""
		if len(closeAtEnd) > 0 {
			trailingWS = jsTrailingSpace(text)
			text = text[:len(text)-len(trailingWS)]
		}
		for _, k := range closeAtEnd {
			m := active.get(k)
			if m == nil {
				m = current.get(k)
			}
			text += markClosing(k, m, modeOf(k))
			active.delete(k)
			delete(modes, k)
		}
		result = append(result, text+trailingWS+middleWS)
	}
	return strings.Join(result, sep)
}
