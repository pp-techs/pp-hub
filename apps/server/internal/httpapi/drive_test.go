package httpapi

import "testing"

// The /content endpoint serves blobs from the app's own origin, so only types
// that cannot execute there may be rendered inline.
func TestInlineDisposition(t *testing.T) {
	cases := map[string]string{
		"application/pdf":           "inline",
		"APPLICATION/PDF":           "inline",
		"text/plain; charset=utf-8": "inline",
		"video/mp4":                 "inline",
		"text/html":                 "attachment",
		"image/svg+xml":             "attachment",
		"application/javascript":    "attachment",
		"application/octet-stream":  "attachment",
		"":                          "attachment",
	}
	for mimeType, want := range cases {
		if got := inlineDisposition(mimeType); got != want {
			t.Errorf("inlineDisposition(%q) = %q, want %q", mimeType, got, want)
		}
	}
}
