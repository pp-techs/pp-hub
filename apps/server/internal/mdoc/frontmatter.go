package mdoc

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A page's icon (an emoji) and cover (`attachment:<hash>`) live in its YAML
// frontmatter as the top-level keys `icon` and `cover`. Only those two keys
// are read or written, one line each; every other line of the block stays
// byte for byte, so this is a line editor, not a YAML library: a key whose
// value it cannot read as a single-line string (block scalar, flow
// collection, alias, tag) counts as unset and is replaced whole when set.

const (
	metaIcon  = "icon"
	metaCover = "cover"
)

var metaKeyLine = regexp.MustCompile(`^(icon|cover)[ \t]*:(?:[ \t]+(.*?))?[ \t]*(?:\r?\n)?$`)

// frontmatterBlock is a frontmatter split into its delimiter lines and the
// lines between them, each with its line terminator.
type frontmatterBlock struct {
	open, close string
	body        []string
}

// splitFrontmatterBlock splits fm, a whole frontmatter block as
// Parsed.Frontmatter holds it; ok=false when fm is not one.
func splitFrontmatterBlock(fm []byte) (frontmatterBlock, bool) {
	if loc := frontmatterRe.FindIndex(fm); loc == nil || loc[1] != len(fm) {
		return frontmatterBlock{}, false
	}
	lines := strings.SplitAfter(string(fm), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 {
		return frontmatterBlock{}, false
	}
	return frontmatterBlock{open: lines[0], close: lines[len(lines)-1], body: lines[1 : len(lines)-1]}, true
}

// FrontmatterMeta returns the page icon and cover a frontmatter block (as
// Parsed.Frontmatter holds it, delimiters included) sets; "" for an absent,
// empty or null key, and for both when fm is nil or not a frontmatter block.
func FrontmatterMeta(fm []byte) (icon, cover string) {
	b, ok := splitFrontmatterBlock(fm)
	if !ok {
		return "", ""
	}
	return b.value(metaIcon), b.value(metaCover)
}

// value is key's value, read from its first top-level line.
func (b frontmatterBlock) value(key string) string {
	for _, line := range b.body {
		if m := metaKeyLine.FindStringSubmatch(line); m != nil && m[1] == key {
			return yamlScalar(m[2])
		}
	}
	return ""
}

// SetFrontmatterMeta returns fm with the icon and cover keys set to the
// given values, a key removed when its value is "". Every other line keeps
// its bytes and order; a new key goes at the end of the block, a block is
// created when fm is nil. It returns fm itself when both values are already
// what fm holds, and nil when the block is left with nothing but blank
// lines. A non-empty fm that is not a frontmatter block is returned as is.
func SetFrontmatterMeta(fm []byte, icon, cover string) []byte {
	if len(fm) == 0 {
		if icon == "" && cover == "" {
			return fm
		}
		fm = []byte("---\n---\n")
	}
	b, ok := splitFrontmatterBlock(fm)
	if !ok {
		return fm
	}
	if b.value(metaIcon) == icon && b.value(metaCover) == cover {
		return fm
	}
	eol := "\n"
	if strings.HasSuffix(b.open, "\r\n") {
		eol = "\r\n"
	}
	b.set(metaIcon, icon, eol)
	b.set(metaCover, cover, eol)
	empty := true
	for _, line := range b.body {
		if strings.TrimSpace(line) != "" {
			empty = false
			break
		}
	}
	if empty {
		return nil
	}
	var out bytes.Buffer
	out.WriteString(b.open)
	for _, line := range b.body {
		out.WriteString(line)
	}
	out.WriteString(b.close)
	return out.Bytes()
}

// set replaces key's first line (and the indented continuation lines under
// it) with `key: value`, dropping any later duplicate; with value "" it
// removes them all. A missing key is appended.
func (b *frontmatterBlock) set(key, value, eol string) {
	written := value == ""
	body := make([]string, 0, len(b.body)+1)
	for i := 0; i < len(b.body); i++ {
		line := b.body[i]
		m := metaKeyLine.FindStringSubmatch(line)
		if m == nil || m[1] != key {
			body = append(body, line)
			continue
		}
		for i+1 < len(b.body) && (strings.HasPrefix(b.body[i+1], " ") || strings.HasPrefix(b.body[i+1], "\t")) {
			i++
		}
		if !written {
			body = append(body, key+": "+yamlQuote(value)+eol)
			written = true
		}
	}
	if !written {
		if n := len(body); n > 0 && !strings.HasSuffix(body[n-1], "\n") {
			body[n-1] += eol
		}
		body = append(body, key+": "+yamlQuote(value)+eol)
	}
	b.body = body
}

// yamlScalar reads a single-line YAML scalar as a string: plain (a trailing
// comment dropped, null as ""), single- or double-quoted. Anything else
// (block scalar, flow collection, alias, tag, unterminated quote) is "".
func yamlScalar(raw string) string {
	if raw == "" {
		return ""
	}
	switch raw[0] {
	case '"':
		s, rest, ok := yamlDoubleQuoted(raw[1:])
		if !ok || !yamlRestIsComment(rest) {
			return ""
		}
		return s
	case '\'':
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			if raw[i] != '\'' {
				b.WriteByte(raw[i])
				continue
			}
			if i+1 < len(raw) && raw[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if !yamlRestIsComment(raw[i+1:]) {
				return ""
			}
			return b.String()
		}
		return ""
	case '|', '>', '[', '{', '&', '*', '!', '%', '@', '`':
		return ""
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "\t#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimRight(raw, " \t")
	switch raw {
	case "~", "null", "Null", "NULL":
		return ""
	}
	return raw
}

func yamlRestIsComment(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	return rest == "" || rest[0] == '#'
}

// yamlDoubleQuoted decodes a double-quoted scalar's body up to its closing
// quote: the decoded string and what follows the quote.
func yamlDoubleQuoted(s string) (string, string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
		default:
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			return "", "", false
		}
		i++
		switch e := s[i]; e {
		case '0':
			b.WriteByte(0)
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 't', '\t':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'v':
			b.WriteByte('\v')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case 'e':
			b.WriteByte(0x1b)
		case ' ', '"', '/', '\\':
			b.WriteByte(e)
		case 'N':
			b.WriteRune(0x85)
		case '_':
			b.WriteRune(0xa0)
		case 'L':
			b.WriteRune(0x2028)
		case 'P':
			b.WriteRune(0x2029)
		case 'x', 'u', 'U':
			n := 2
			if e == 'u' {
				n = 4
			} else if e == 'U' {
				n = 8
			}
			if i+1+n > len(s) {
				return "", "", false
			}
			v, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if err != nil {
				return "", "", false
			}
			r := rune(v)
			// A JSON-style surrogate pair: \uD83C\uDF89.
			if r >= 0xd800 && r < 0xdc00 && e == 'u' && i+1+n+6 <= len(s) && s[i+1+n] == '\\' && s[i+2+n] == 'u' {
				if lo, err := strconv.ParseUint(s[i+3+n:i+7+n], 16, 32); err == nil && lo >= 0xdc00 && lo < 0xe000 {
					r = 0x10000 + (r-0xd800)<<10 + (rune(lo) - 0xdc00)
					i += 6
				}
			}
			if !utf8.ValidRune(r) {
				return "", "", false
			}
			b.WriteRune(r)
			i += n
		default:
			return "", "", false
		}
	}
	return "", "", false
}

// yamlQuote writes v as a YAML scalar that reads back as exactly the string
// v: plain when that is unambiguous, otherwise double-quoted (a JSON string
// is a valid YAML double-quoted scalar).
func yamlQuote(v string) string {
	if yamlPlainSafe(v) {
		return v
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}

// yamlPlainSafe reports whether v written plain reads back as the string v
// under YAML 1.1 and 1.2 alike: no indicator first, nothing a mapping or
// comment could start inside, no surrounding blanks, no control
// characters, and nothing that resolves to null, a boolean or a number.
func yamlPlainSafe(v string) bool {
	if v == "" || v != strings.TrimSpace(v) {
		return false
	}
	if strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`.+0123456789~", rune(v[0])) {
		return false
	}
	if strings.Contains(v, ": ") || strings.Contains(v, " #") || strings.HasSuffix(v, ":") {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == 0xfeff || r == 0x2028 || r == 0x2029 || r == 0x85 {
			return false
		}
	}
	switch strings.ToLower(v) {
	case "null", "true", "false", "yes", "no", "on", "off", "y", "n", ".nan", ".inf":
		return false
	}
	return true
}
