package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func linkShare(perm string) store.Share {
	return store.Share{Kind: store.ShareKindLink, Perm: store.SharePerm(perm)}
}

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

// A link that grants only "view" must refuse downloads, and say so as 403
// rather than silently serving the bytes.
func TestLinkAllowsDownload(t *testing.T) {
	cases := map[string]bool{"view": false, "download": true, "edit": true}
	for perm, want := range cases {
		rec := httptest.NewRecorder()
		got := linkAllowsDownload(rec, linkShare(perm))
		if got != want {
			t.Errorf("perm=%s: allowed = %v, want %v", perm, got, want)
		}
		if !want && rec.Code != 403 {
			t.Errorf("perm=%s: status = %d, want 403", perm, rec.Code)
		}
	}
}

// A direct (non-link) share reaching the link path must be refused outright:
// LinkPerm errors for it, and a bug that treated the error as "allowed" would
// hand anonymous callers an authenticated user's grant.
func TestLinkAllowsDownloadRejectsDirectShare(t *testing.T) {
	rec := httptest.NewRecorder()
	sh := linkShare("edit")
	sh.Kind = "direct"

	if linkAllowsDownload(rec, sh) {
		t.Error("a direct share must not satisfy the link download check")
	}
	if rec.Code != 403 {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
