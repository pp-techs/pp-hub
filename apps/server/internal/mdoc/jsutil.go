package mdoc

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JavaScript semantics the serializer depends on, so output stays
// byte-identical to slash-editor's.

// jsIsSpace is JavaScript's `\s` / String.prototype.trim whitespace set.
func jsIsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, jsIsSpace) }

// jsLeadingSpace returns the run of leading whitespace (`/^(\s+)/`).
func jsLeadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeftFunc(s, jsIsSpace))]
}

// jsTrailingSpace returns the run of trailing whitespace (`/(\s+)$/`).
func jsTrailingSpace(s string) string {
	return s[len(strings.TrimRightFunc(s, jsIsSpace)):]
}

// jsCollapseSpace is `s.replace(/\s+/g, " ").trim()`.
func jsCollapseSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if jsIsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return jsTrim(b.String())
}

// jsLen is String.prototype.length: UTF-16 code units.
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsNumber formats a float64 the way JavaScript's Number#toString does for
// the values documents carry (integers, ordinary decimals).
func jsNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null" // JSON.stringify
	}
	if f == 0 {
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		mant, exp, _ := strings.Cut(s, "e")
		if exp[0] == '+' {
			exp = exp[1:]
		} else if exp[0] == '-' {
			exp = "-" + strings.TrimLeft(exp[1:], "0")
		}
		exp = strings.TrimLeft(exp, "0")
		if strings.HasPrefix(exp, "-") {
			return mant + "e" + exp
		}
		return mant + "e+" + exp
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// jsJSON is JSON.stringify for the JSON-decoded values attributes hold.
// Object keys keep the order given by keys (JSON.stringify follows insertion
// order); nested maps fall back to sorted keys.
func jsJSON(v any) string {
	var b strings.Builder
	writeJSJSON(&b, v)
	return b.String()
}

func writeJSJSON(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		writeJSString(b, x)
	case float64:
		b.WriteString(jsNumber(x))
	case float32:
		b.WriteString(jsNumber(float64(x)))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSJSON(b, e)
		}
		b.WriteByte(']')
	case orderedAttrs:
		b.WriteByte('{')
		for i, kv := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSString(b, kv.key)
			b.WriteByte(':')
			writeJSJSON(b, kv.value)
		}
		b.WriteByte('}')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make(orderedAttrs, 0, len(keys))
		for _, k := range keys {
			ordered = append(ordered, attrKV{k, x[k]})
		}
		writeJSJSON(b, ordered)
	default:
		b.WriteString(fmt.Sprint(x))
	}
}

type attrKV struct {
	key   string
	value any
}

type orderedAttrs []attrKV

func writeJSString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
