package preview

import "testing"

// kindForMime must stay in lockstep with previewKind in
// apps/web/src/lib/api/utils.ts (see apps/web/tests/preview-kind.test.ts) —
// previews are stored under the kind picked here and looked up by the kind the
// client asks for, so any drift 404s as "preview not ready".
// See docs/architecture/preview-pipeline.md.
func TestKindForMime(t *testing.T) {
	cases := map[string]string{
		"application/pdf":         JobPDF,
		"image/png":               JobThumbnail,
		"image/jpeg":              JobThumbnail,
		"image/gif":               JobThumbnail,
		"image/webp":              JobThumbnail,
		"video/mp4":               JobVideo,
		"video/webm":              JobVideo,
		"video/quicktime":         JobVideo,
		"application/rtf":         JobText,
		"text/plain":              JobText,
		"text/markdown":           JobText,
		"text/csv; charset=utf-8": JobText,
		// No preview: .epub is a zip, office formats open in the editor.
		"application/epub+zip": "",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "",
		"image/svg+xml":    "",
		"video/x-matroska": "",
		"application/zip":  "",
		"":                 "",
	}
	for mime, want := range cases {
		if got := kindForMime(mime); got != want {
			t.Errorf("kindForMime(%q) = %q, want %q", mime, got, want)
		}
	}
}
