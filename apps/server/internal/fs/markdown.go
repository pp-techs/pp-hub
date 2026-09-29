package fs

import (
	"mime"
	"strings"
)

// MarkdownMIME is the type of a block document (M39): a `.md` file, edited in
// the block editor rather than previewed as text.
const MarkdownMIME = "text/markdown"

func init() {
	// A version's type comes from its name (mime.TypeByExtension), and Go's
	// builtin table has no `.md`: it falls back to the host's mime.types,
	// which says text/markdown, text/x-markdown or nothing depending on the
	// distribution — so whether a file opened in the block editor would have
	// depended on the machine that stored it. As for every text/* type, the
	// package appends "; charset=utf-8".
	_ = mime.AddExtensionType(".md", MarkdownMIME) // fails only for an extension without a dot
}

// IsMarkdown reports whether a stored version's type is a block document's,
// whatever parameters (the charset) it carries.
func IsMarkdown(mimeType string) bool {
	base, _, _ := strings.Cut(mimeType, ";")
	return strings.EqualFold(strings.TrimSpace(base), MarkdownMIME)
}
