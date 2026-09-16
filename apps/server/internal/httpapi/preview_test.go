package httpapi

import (
	"strings"
	"testing"
)

// The preview response is ACL-gated, so it must never be cacheable by a shared
// proxy — that would hand one company's thumbnail to a member of another.
func TestPreviewCacheControlIsPrivateAndImmutable(t *testing.T) {
	for _, want := range []string{"private", "immutable", "max-age=31536000"} {
		if !strings.Contains(previewCacheControl, want) {
			t.Errorf("Cache-Control %q is missing %q", previewCacheControl, want)
		}
	}
	if strings.Contains(previewCacheControl, "public") {
		t.Errorf("Cache-Control %q must not be public", previewCacheControl)
	}
}

func TestPreviewETagIsQuoted(t *testing.T) {
	got := previewETag("abc123")
	if got != `"abc123"` {
		t.Errorf("previewETag = %s, want %q", got, `"abc123"`)
	}
}

// Without a correct If-None-Match comparison the 304 path never fires and
// every grid scroll re-downloads every thumbnail.
func TestETagMatches(t *testing.T) {
	etag := previewETag("abc123")
	cases := []struct {
		header string
		want   bool
	}{
		{`"abc123"`, true},
		{`W/"abc123"`, true},                 // weak validator on revalidation
		{`"other", "abc123"`, true},          // client sent a list
		{`"other","abc123"`, true},           // …without spaces
		{"*", true},                          // matches anything cached
		{`"other"`, false},                   // a different version
		{"", false},                          // first request
		{`"abc123x"`, false},                 // prefix must not match
		{`"ABC123"`, true},                   // hex hash, case-insensitive
		{`W/"other", W/"abc123"`, true},      // weak list
		{`"a", "b", "c"`, false},             // no match in list
	}
	for _, c := range cases {
		if got := etagMatches(c.header, etag); got != c.want {
			t.Errorf("etagMatches(%q, %s) = %v, want %v", c.header, etag, got, c.want)
		}
	}
}
