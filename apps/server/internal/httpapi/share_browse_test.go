package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A non-ASCII or quote-bearing filename must survive the Content-Disposition
// header intact. The hand-built header this replaced produced filename=""x""
// via strconv.Quote, and browsers saved files with the quotes in the name.
func TestContentDisposition(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{"plain", "report.pdf", `attachment; filename=report.pdf`},
		{"spaces", "Q1 report.pdf", `attachment; filename="Q1 report.pdf"`},
		{"quote", `we"ird.pdf`, `attachment; filename="we\"ird.pdf"`},
	}
	for _, c := range cases {
		got := contentDisposition("attachment", c.filename)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// Non-ASCII must not silently drop the disposition itself.
	if got := contentDisposition("attachment", "отчёт.pdf"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("non-ascii name lost the disposition: %q", got)
	}
}

// A link share respects can_download: if false, only preview is allowed.
func TestLinkAllowsDownload(t *testing.T) {
	cases := []struct {
		canDownload bool
		want        bool
	}{
		{false, false},
		{true, true},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		sh := store.Share{Kind: store.ShareKindLink, CanDownload: tc.canDownload}
		got := linkAllowsDownload(rec, sh)
		if got != tc.want {
			t.Errorf("CanDownload=%v: allowed=%v, want %v", tc.canDownload, got, tc.want)
		}
		if !tc.want && rec.Code != 403 {
			t.Errorf("CanDownload=%v: status=%d, want 403", tc.canDownload, rec.Code)
		}
	}
}

// A direct (non-link) share reaching the link path must be refused outright.
func TestLinkAllowsDownloadRejectsDirectShare(t *testing.T) {
	rec := httptest.NewRecorder()
	sh := store.Share{Kind: store.ShareKindDirect, CanDownload: true}

	if linkAllowsDownload(rec, sh) {
		t.Error("a direct share must not satisfy the link download check")
	}
	if rec.Code != 403 {
		t.Errorf("status=%d, want 403", rec.Code)
	}
}
