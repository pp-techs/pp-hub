package preview

import (
	"regexp"
	"strings"
)

// svgFallbackColor stands in for any color oksvg cannot parse: the literal
// currentColor token, an unresolvable var(), or a color-mix() this package
// makes no attempt to compute. Black matches what a browser shows for
// currentColor with no inherited color, and is as reasonable a default as
// any for the rest — the alternative is no thumbnail at all.
const svgFallbackColor = "#000000"

// resolveCSSColors makes oksvg-parseable colors out of the subset of CSS
// Color Module / Custom Properties syntax it has zero support for: oksvg's
// color parser hard-errors ("param mismatch") the instant it sees the
// literal text "var(" or "color-mix(" in a fill/stroke value, even when the
// custom property it references is a plain, resolvable hex color declared
// right there on the root element. That pattern — a handful of base colors
// as custom properties on <svg style="--fg:...">, referenced by var() from
// presentation attributes — is exactly how generated charts/diagrams theme
// themselves, so without this every such file fails to render at all.
//
// This is not a CSS engine: no cascade, no specificity, no inheritance, no
// computed color mixing. It only does two textual passes:
//
//  1. Resolve var(--name) against literal `--name: value;` declarations on
//     the root <svg style="..."> attribute. A few iterations let a chain of
//     var-referencing-var resolve as each pass turns one more name literal.
//  2. Flatten anything still standing afterward — an undefined var(), a
//     var()'s own CSS fallback argument when that is itself unresolvable, or
//     a bare color-mix() — to svgFallbackColor, so the file still renders
//     with a wrong-but-present color rather than not rendering at all.
func resolveCSSColors(svg string) string {
	for range 3 {
		// Re-extracted every pass: pass 1 may turn a chained declaration like
		// --b: var(--a) into a literal, which only the next pass's props map
		// can then use to resolve var(--b) elsewhere in the document.
		props := rootCustomProps(svg)
		next, changed := substituteVars(svg, props)
		svg = next
		if !changed {
			break
		}
	}
	return collapseColorFuncs(svg)
}

var (
	svgOpenTagRe = regexp.MustCompile(`(?s)<svg\b[^>]*>`)
	styleAttrRe  = regexp.MustCompile(`\bstyle\s*=\s*"([^"]*)"`)
	customPropRe = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:\s*([^;"]+)`)
)

// rootCustomProps reads literal `--name: value;` pairs off the root <svg>
// element's own style attribute — deliberately not any other element's, and
// not the <style> block, since resolving either correctly needs the CSS
// cascade this package does not implement.
func rootCustomProps(svg string) map[string]string {
	tag := svgOpenTagRe.FindString(svg)
	if tag == "" {
		return nil
	}
	m := styleAttrRe.FindStringSubmatch(tag)
	if m == nil {
		return nil
	}
	props := map[string]string{}
	for _, kv := range customPropRe.FindAllStringSubmatch(m[1], -1) {
		props[kv[1]] = strings.TrimSpace(kv[2])
	}
	return props
}

// substituteVars replaces each var(...) occurrence it can resolve — either
// the named custom property has a literal value in props, or the var()'s own
// fallback argument is itself literal — and reports whether it changed
// anything, so the caller knows whether another pass could resolve further.
func substituteVars(svg string, props map[string]string) (string, bool) {
	var b strings.Builder
	changed := false
	i := 0
	for {
		idx := strings.Index(svg[i:], "var(")
		if idx == -1 {
			b.WriteString(svg[i:])
			break
		}
		start := i + idx
		open := start + len("var") // index of '('
		end := matchParen(svg, open)
		if end == -1 {
			b.WriteString(svg[i:])
			break
		}
		b.WriteString(svg[i:start])
		name, fallback := splitVarArgs(svg[open+1 : end])
		switch {
		case props[name] != "" && !containsColorFunc(props[name]):
			b.WriteString(props[name])
			changed = true
		case fallback != "" && !containsColorFunc(fallback):
			// The property is undefined (or we only resolve root-level
			// props at all) — honor var()'s own CSS fallback argument,
			// which is what a browser would fall back to as well.
			b.WriteString(fallback)
			changed = true
		default:
			// Leave it for collapseColorFuncs: still unresolvable.
			b.WriteString(svg[start : end+1])
		}
		i = end + 1
	}
	return b.String(), changed
}

// splitVarArgs splits a var() call's inner text ("--name" or
// "--name, fallback") on the first top-level comma — the fallback argument
// may itself contain commas inside a nested function call, e.g.
// var(--x, color-mix(in srgb, red, blue)).
func splitVarArgs(inner string) (name, fallback string) {
	depth := 0
	for i, r := range inner {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				name = trimVarName(inner[:i])
				fallback = strings.TrimSpace(inner[i+1:])
				return name, fallback
			}
		}
	}
	return trimVarName(inner), ""
}

func trimVarName(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "--")
}

func containsColorFunc(s string) bool {
	return strings.Contains(s, "var(") || strings.Contains(s, "color-mix(")
}

// collapseColorFuncs replaces every remaining var(...) or color-mix(...)
// call — balanced parens and all, so a color-mix() nested inside an
// unresolved var()'s fallback is removed along with it — with
// svgFallbackColor.
func collapseColorFuncs(svg string) string {
	var b strings.Builder
	i := 0
	for {
		start := nextColorFuncStart(svg, i)
		if start == -1 {
			b.WriteString(svg[i:])
			return b.String()
		}
		open := strings.IndexByte(svg[start:], '(') + start
		end := matchParen(svg, open)
		if end == -1 {
			b.WriteString(svg[i:])
			return b.String()
		}
		b.WriteString(svg[i:start])
		b.WriteString(svgFallbackColor)
		i = end + 1
	}
}

func nextColorFuncStart(svg string, from int) int {
	vi := indexFrom(svg, "var(", from)
	ci := indexFrom(svg, "color-mix(", from)
	switch {
	case vi == -1:
		return ci
	case ci == -1:
		return vi
	case vi < ci:
		return vi
	default:
		return ci
	}
}

func indexFrom(s, sub string, from int) int {
	idx := strings.Index(s[from:], sub)
	if idx == -1 {
		return -1
	}
	return idx + from
}

// matchParen returns the index of the ')' matching the '(' at open, or -1 if
// the string ends first (malformed input — the caller gives up on the rest).
func matchParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
