package mdoc

import (
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Relative references: a document names a file beside it by path —
// `![](images/a.png)`, `[spec](../spec.pdf)` — the way a README in a
// repository does, and the way an export writes its assets/. The markdown
// keeps the path as written; the server resolves it against the document's
// place in the tree whenever it renders (block-documents.md, Relative
// references).

// Bounds on a relative reference: far above any real tree, low enough that
// resolving one costs a handful of queries.
const (
	maxRelativeLen      = 1024
	maxRelativeSegments = 32
)

// relativeAttrs are the attributes that may hold a relative reference, per
// node type. Link marks' href is the other place.
var relativeAttrs = map[string][]string{
	"image": {"src"},
	"file":  {"src"},
	"video": {"src", "poster"},
}

// RelativePath reads ref as a relative reference and returns its path
// segments: `.` and empty segments dropped, `name/..` folded away, `..` kept
// only at the front, each segment percent-decoded and in Unicode NFC (the
// form names are compared in). The query and fragment are not part of the
// path. ok is false for anything else — a scheme or host (`attachment:`,
// `https:`, `data:`, `//cdn`), a rooted path, a bare `#fragment` or `?query`,
// a path that names a folder (a trailing `/`, `.` or `..`), an encoded `/`
// inside a segment, or a reference over the bounds.
func RelativePath(ref string) ([]string, bool) {
	if ref == "" || len(ref) > maxRelativeLen ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, `\`) {
		return nil, false
	}
	p := ref
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || hasScheme(p) {
		return nil, false
	}
	parts := strings.Split(p, "/")
	segs := make([]string, 0, len(parts))
	for i, raw := range parts {
		seg, err := url.PathUnescape(raw)
		if err != nil || strings.ContainsAny(seg, `/\`) {
			return nil, false
		}
		seg = norm.NFC.String(seg)
		last := i == len(parts)-1
		switch seg {
		case "", ".":
			if last {
				return nil, false
			}
		case "..":
			if last {
				return nil, false
			}
			if n := len(segs); n > 0 && segs[n-1] != ".." {
				segs = segs[:n-1]
			} else {
				segs = append(segs, "..")
			}
		default:
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 || len(segs) > maxRelativeSegments {
		return nil, false
	}
	return segs, true
}

// hasScheme reports whether p starts with a URL scheme (RFC 3986: a letter,
// then letters, digits, `+`, `-` or `.`, then `:`) — `C:\…` included, which
// is no relative reference either. A colon after the first `/` is part of a
// name (`./a:b.png`).
func hasScheme(p string) bool {
	colon := strings.IndexByte(p, ':')
	if colon <= 0 {
		return false
	}
	if slash := strings.IndexByte(p, '/'); slash >= 0 && slash < colon {
		return false
	}
	for i, r := range p[:colon] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// IsRelative reports whether ref is a relative reference.
func IsRelative(ref string) bool {
	_, ok := RelativePath(ref)
	return ok
}

// RelativeRefs returns doc's relative references — image, file and video
// sources, video posters and link targets — as written, in document order
// without duplicates.
func RelativeRefs(doc Node) []string {
	var out []string
	seen := map[string]bool{}
	add := func(v any) {
		if s, ok := v.(string); ok && !seen[s] && IsRelative(s) {
			seen[s] = true
			out = append(out, s)
		}
	}
	walk(&doc, func(n *Node) {
		for _, k := range relativeAttrs[n.Type] {
			add(n.Attrs[k])
		}
		for _, m := range n.Marks {
			if m.Type == "link" {
				add(m.Attrs["href"])
			}
		}
	})
	return out
}

// MapRelative returns a copy of doc with every relative reference for which
// fn answers ok replaced by what it answers; doc itself is left untouched.
func MapRelative(doc Node, fn func(ref string) (string, bool)) Node {
	mapped := func(v any) (string, bool) {
		s, ok := v.(string)
		if !ok || !IsRelative(s) {
			return "", false
		}
		return fn(s)
	}
	out := doc
	if keys := relativeAttrs[doc.Type]; len(keys) > 0 && doc.Attrs != nil {
		attrs := make(map[string]any, len(doc.Attrs))
		for k, v := range doc.Attrs {
			attrs[k] = v
		}
		for _, k := range keys {
			if to, ok := mapped(attrs[k]); ok {
				attrs[k] = to
			}
		}
		out.Attrs = attrs
	}
	if doc.Marks != nil {
		out.Marks = make([]Mark, len(doc.Marks))
		for i, m := range doc.Marks {
			out.Marks[i] = m
			if m.Type != "link" {
				continue
			}
			if to, ok := mapped(m.Attrs["href"]); ok {
				attrs := make(map[string]any, len(m.Attrs))
				for k, v := range m.Attrs {
					attrs[k] = v
				}
				attrs["href"] = to
				out.Marks[i].Attrs = attrs
			}
		}
	}
	if doc.Content != nil {
		out.Content = make([]Node, len(doc.Content))
		for i, c := range doc.Content {
			out.Content[i] = MapRelative(c, fn)
		}
	}
	return out
}
