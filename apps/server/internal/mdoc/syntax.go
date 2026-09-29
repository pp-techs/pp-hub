package mdoc

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Ports of slash-editor's own markdown tokenizers (block-kit's markers,
// media lines, callouts, toggles, columns) and of @tiptap/extension-list's
// ordered/task list tokenizers. marked runs these before its built-in rules
// at every block start, so Parse runs them before handing a block to
// goldmark. Regexes are kept as close to the JS originals as RE2 allows.

const lineEnd = `[ \t]*(?:\n|$)`

var markerRegexes = map[string]*regexp.Regexp{}

func markerPattern(typ string, closing bool) string {
	if closing {
		return ` {0,3}<!-- /slash:` + regexp.QuoteMeta(typ) + ` -->`
	}
	return ` {0,3}<!-- slash:` + regexp.QuoteMeta(typ) + `(?: (\{.*?\}))? -->`
}

func markerRegex(typ string) *regexp.Regexp {
	re, ok := markerRegexes[typ]
	if !ok {
		re = regexp.MustCompile(`^` + markerPattern(typ, false) + lineEnd)
		markerRegexes[typ] = re
	}
	return re
}

type marker struct {
	raw   string
	attrs map[string]any
}

// readMarker reads a `type` marker line at the very start of src; nil when
// there is none or its payload is not a JSON object.
func readMarker(src, typ string) *marker {
	m := markerRegex(typ).FindStringSubmatchIndex(src)
	if m == nil {
		return nil
	}
	attrs := map[string]any{}
	if m[2] >= 0 {
		var v any
		if err := json.Unmarshal([]byte(src[m[2]:m[3]]), &v); err != nil {
			return nil
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		attrs = obj
	}
	return &marker{raw: src[:m[1]], attrs: attrs}
}

var (
	anyMarkerLine   = regexp.MustCompile(`^ {0,3}<!-- /?slash:[^\n]*?-->[ \t]*(?:\n|$)`)
	inlineMarker    = regexp.MustCompile(`^<!-- /?slash:[^\n]*?-->`)
	linkLine        = regexp.MustCompile(`^ {0,3}(!?)\[((?:\\.|[^\\\]\n])*)\]\((?:<([^<>\n]*)>|([^\s()<>]*))(?:[ \t]+"(?:\\.|[^"\\\n])*")?\)` + lineEnd)
	blankRest       = regexp.MustCompile(`^[ \t]*(?:\n|$)`)
	fenceOpen       = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	alertRe         = regexp.MustCompile(`(?i)^( {0,3}> ?\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\][ \t]*(?:\n|$))((?: {0,3}>[^\n]*(?:\n|$))*)`)
	alertBodyPrefix = regexp.MustCompile(`(?m)^ {0,3}> ?`)
	detailsOpen     = regexp.MustCompile(`(?i)^ {0,3}<details(\s+open)?\s*>[ \t]*\n?[ \t]*<summary>(.*?)</summary>[ \t]*(?:\n|$)`)
	detailsHeading  = regexp.MustCompile(`(?i)^<h([1-6])>(.*)</h([1-6])>$`)
	unescapeAny     = regexp.MustCompile(`\\(.)`)
)

var iconByAlert = map[string]string{
	"TIP":       "💡",
	"NOTE":      "ℹ️",
	"IMPORTANT": "❗",
	"WARNING":   "⚠️",
	"CAUTION":   "⛔",
}

type linkLineMatch struct {
	raw   string
	image bool
	label string
	href  string
}

func readLinkLine(src string) *linkLineMatch {
	m := linkLine.FindStringSubmatchIndex(src)
	if m == nil {
		return nil
	}
	href := ""
	if m[6] >= 0 {
		href = src[m[6]:m[7]]
	} else if m[8] >= 0 {
		href = src[m[8]:m[9]]
	}
	return &linkLineMatch{
		raw:   src[:m[1]],
		image: m[3] > m[2],
		label: unescapeAny.ReplaceAllString(src[m[4]:m[5]], "$1"),
		href:  href,
	}
}

// blockSyntax is scanBlock's open/close/separator line patterns.
type blockSyntax struct{ open, close, separator *regexp.Regexp }

func markerLinePattern(typ string, closing bool) *regexp.Regexp {
	return regexp.MustCompile(`^` + markerPattern(typ, closing) + `[ \t]*$`)
}

var (
	columnsSyntax = blockSyntax{
		open:      markerLinePattern("columns", false),
		close:     markerLinePattern("columns", true),
		separator: markerLinePattern("column", false),
	}
	detailsSyntax = blockSyntax{
		open:  regexp.MustCompile(`(?i)^ {0,3}<details[\s>]`),
		close: regexp.MustCompile(`(?i)^ {0,3}</details>`),
	}
)

// unpad strips the one blank line the serializer writes on each side of a
// structural line.
func unpad(part string) string {
	if i := strings.IndexByte(part, '\n'); i >= 0 && strings.Trim(part[:i], " \t") == "" {
		part = part[i+1:]
	}
	if strings.HasSuffix(part, "\n") {
		body := part[:len(part)-1]
		if j := strings.LastIndexByte(body, '\n'); j >= 0 && strings.Trim(body[j+1:], " \t") == "" {
			part = body[:j] + "\n"
		}
	}
	return part
}

// scanBlock walks src to the close line balancing an already-consumed
// opener, splitting at depth-0 separator lines; lines inside fenced code
// never count. ok is false when the construct never closes.
func scanBlock(src string, syntax blockSyntax) (length int, parts []string, ok bool) {
	depth := 0
	fence := ""
	partStart, offset := 0, 0
	for offset < len(src) {
		nl := strings.IndexByte(src[offset:], '\n')
		end, lineEnd := len(src), len(src)
		if nl >= 0 {
			lineEnd = offset + nl
			end = lineEnd + 1
		}
		line := src[offset:lineEnd]
		switch {
		case fence != "":
			if strings.HasPrefix(jsTrim(line), fence) {
				fence = ""
			}
		case fenceOpen.MatchString(line):
			fence = fenceOpen.FindStringSubmatch(line)[1]
		case syntax.open.MatchString(line):
			depth++
		case syntax.close.MatchString(line):
			if depth == 0 {
				return end, append(parts, unpad(src[partStart:offset])), true
			}
			depth--
		case depth == 0 && syntax.separator != nil && syntax.separator.MatchString(line):
			parts = append(parts, unpad(src[partStart:offset]))
			partStart = end
		}
		offset = end
	}
	return 0, nil, false
}

// ---- ordered lists (@tiptap/extension-list OrderedList.markdownTokenizer)

var (
	orderedItemRe  = regexp.MustCompile(`^(\s*)(\d+|[ivxlcdmIVXLCDM]+|[a-zA-Z]{1,2})([.)])\s+(.*)$`)
	indentedLineRe = regexp.MustCompile(`^\s`)
	interrupters   = []*regexp.Regexp{
		regexp.MustCompile(`^#{1,6}(?:\s|$)`),
		regexp.MustCompile(`^[-+*]\s+`),
		regexp.MustCompile("^(?:```|~~~)"),
		regexp.MustCompile(`^\$\$`),
		regexp.MustCompile(`^(?:(?:-[ \t]*){3,}|(?:_[ \t]*){3,}|(?:\*[ \t]*){3,})$`),
	}
	bulletItemRe    = interrupters[1]
	headingLineRe   = interrupters[0]
	codeFenceRe     = interrupters[2]
	blockMathRe     = interrupters[3]
	thematicBreakRe = interrupters[4]
	quoteLineRe     = regexp.MustCompile(`^>\s?`)
	romanRe         = regexp.MustCompile(`^[ivxlcdmIVXLCDM]+$`)
	lowerAlphaRe    = regexp.MustCompile(`^[a-z]{1,2}$`)
	upperAlphaRe    = regexp.MustCompile(`^[A-Z]{1,2}$`)
	digitsRe        = regexp.MustCompile(`^\d+$`)
)

func fromRoman(s string) int {
	s = strings.ToLower(s)
	total := 0
	for i := 0; i < len(s); {
		matched := false
		for _, rn := range romanNumerals {
			if strings.HasPrefix(s[i:], rn.numeral) {
				total += rn.value
				i += len(rn.numeral)
				matched = true
				break
			}
		}
		if !matched {
			return 0
		}
	}
	return total
}

func isValidRoman(m string) bool {
	if !romanRe.MatchString(m) {
		return false
	}
	v := fromRoman(m)
	if v <= 0 {
		return false
	}
	if m == strings.ToLower(m) {
		return romanNumeral(v) == m
	}
	return strings.ToUpper(romanNumeral(v)) == m
}

// detectMarkerType returns "", "i", "I", "a" or "A".
func detectMarkerType(m string) string {
	if m == "" || digitsRe.MatchString(m) {
		return ""
	}
	if isValidRoman(m) {
		if m == strings.ToLower(m) {
			return "i"
		}
		return "I"
	}
	if lowerAlphaRe.MatchString(m) {
		return "a"
	}
	if upperAlphaRe.MatchString(m) {
		return "A"
	}
	return ""
}

func fromAlpha(m string) int {
	l := strings.ToLower(m)
	switch len(l) {
	case 1:
		return int(l[0]-'a') + 1
	case 2:
		return (int(l[0]-'a')+1)*26 + int(l[1]-'a') + 1
	}
	return 0
}

func markerToStart(m string) int {
	if digitsRe.MatchString(m) {
		return jsParseInt(m)
	}
	switch detectMarkerType(m) {
	case "i", "I":
		return fromRoman(m)
	case "a", "A":
		if s := fromAlpha(m); s > 0 {
			return s
		}
		return 1
	}
	return 1 // parseInt(non-digits) is NaN
}

type orderedItem struct {
	indent       int
	number       int
	typ          string
	contentLines []string
}

func isBlockContentLine(line string) bool {
	t := strings.TrimLeftFunc(line, jsIsSpace)
	return bulletItemRe.MatchString(t) || orderedItemRe.MatchString(t) || headingLineRe.MatchString(t) ||
		(thematicBreakRe.MatchString(t) && !strings.HasPrefix(t, "-")) || quoteLineRe.MatchString(t) ||
		codeFenceRe.MatchString(t) || blockMathRe.MatchString(t)
}

func interruptsLazyContinuation(line string) bool {
	for _, re := range interrupters {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// collectOrderedListItems returns the flat items and how many lines they use.
func collectOrderedListItems(lines []string) ([]orderedItem, int) {
	var items []orderedItem
	i, consumed := 0, 0
	for i < len(lines) {
		m := orderedItemRe.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		indent, mark, content := len(m[1]), m[2], m[4]
		item := orderedItem{indent: indent, contentLines: []string{content}}
		if digitsRe.MatchString(mark) || (mark != "" && mark[0] >= '0' && mark[0] <= '9') {
			item.number = jsParseInt(mark)
		} else {
			item.typ = detectMarkerType(mark)
			item.number = markerToStart(mark)
		}
		next := i + 1
		sawBlank := false
		for next < len(lines) {
			line := lines[next]
			if orderedItemRe.MatchString(line) {
				break
			}
			if jsTrim(line) == "" {
				item.contentLines = append(item.contentLines, "")
				sawBlank = true
				next++
			} else if indentedLineRe.MatchString(line) {
				lead := len(line) - len(strings.TrimLeftFunc(line, jsIsSpace))
				contentIndent := indent + len(mark) + 1
				item.contentLines = append(item.contentLines, line[min(lead, contentIndent):])
				next++
			} else {
				if sawBlank || interruptsLazyContinuation(line) {
					break
				}
				item.contentLines = append(item.contentLines, line)
				next++
			}
		}
		items = append(items, item)
		consumed = next
		i = next
	}
	return items, consumed
}

// splitItemContent separates an item's paragraph lines from what follows.
func splitItemContent(lines []string) (para, block []string) {
	boundary := false
	for _, line := range lines {
		switch {
		case boundary:
			block = append(block, line)
		case jsTrim(line) == "":
			boundary = true
			block = append(block, line)
		case len(para) > 0 && isBlockContentLine(line):
			boundary = true
			block = append(block, line)
		default:
			para = append(para, line)
		}
	}
	return para, block
}

// ---- task lists (@tiptap/core parseIndentedBlocks + TaskList tokenizer)

var (
	taskItemRe    = regexp.MustCompile(`^(\s*)([-+*])\s+\[([ xX])\]\s+(.*)$`)
	taskStartRe   = regexp.MustCompile(`^\s*[-+*]\s+\[([ xX])\]\s+`)
	leadingSpaces = regexp.MustCompile(`^(\s*)`)
)

func leadingWidth(line string) int { return len(leadingSpaces.FindString(line)) }

type taskItemToken struct {
	checked bool
	main    string
	nested  string // dedented nested content, "" if none
}

// parseIndentedTasks is parseIndentedBlocks with the task item pattern: the
// items it reads and the raw text they span.
func parseIndentedTasks(src string) ([]taskItemToken, int) {
	lines := strings.Split(src, "\n")
	var items []taskItemToken
	total := 0
	i := 0
	for i < len(lines) {
		m := taskItemRe.FindStringSubmatch(lines[i])
		if m == nil {
			if len(items) > 0 {
				break
			}
			if jsTrim(lines[i]) == "" {
				total += len(lines[i]) + 1
				i++
				continue
			}
			return nil, 0
		}
		indent := len(m[1])
		total += len(lines[i]) + 1
		content := []string{}
		i++
		for i < len(lines) {
			next := lines[i]
			if jsTrim(next) == "" {
				k := -1
				for j := i + 1; j < len(lines); j++ {
					if jsTrim(lines[j]) != "" {
						k = j
						break
					}
				}
				if k == -1 || leadingWidth(lines[k]) <= indent {
					break
				}
				content = append(content, next)
				total += len(next) + 1
				i++
				continue
			}
			if leadingWidth(next) > indent {
				content = append(content, next)
				total += len(next) + 1
				i++
				continue
			}
			break
		}
		item := taskItemToken{checked: strings.EqualFold(m[3], "x"), main: m[4]}
		if len(content) > 0 {
			dedented := make([]string, len(content))
			for k, line := range content {
				dedented[k] = jsSlice(line, indent+2)
			}
			if joined := strings.Join(dedented, "\n"); jsTrim(joined) != "" {
				item.nested = joined
			}
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, 0
	}
	return items, total
}

// jsSlice is String.prototype.slice(start) on byte offsets clamped to len.
func jsSlice(s string, start int) string {
	if start >= len(s) {
		return ""
	}
	return s[start:]
}
