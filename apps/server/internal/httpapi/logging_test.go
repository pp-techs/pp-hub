package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The two bearer credentials this API carries in URLs must not reach the log.
// A share token is the entire authorization for anonymous access to a file,
// and only its SHA-256 is stored — logging the plaintext beside it would make
// that storage decision pointless.
func TestRedactURIHidesBearerCredentials(t *testing.T) {
	cases := map[string]string{
		"/s/abc123def/download":                           "/s/REDACTED/download",
		"/s/abc123def":                                    "/s/REDACTED",
		"/s/abc123def/content?node=xyz":                   "/s/REDACTED/content?node=xyz",
		"/wopi/files/n1/contents?access_token=secret&x=1": "/wopi/files/n1/contents?access_token=REDACTED&x=1",
	}
	for in, want := range cases {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := redactURI(u); got != want {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}

// Everything else must be left alone, and cheaply: this runs on every
// request.
func TestRedactURILeavesOrdinaryPathsAlone(t *testing.T) {
	for _, in := range []string{
		"/api/v1/spaces",
		"/api/v1/nodes/abc/content?v=3",
		"/",
		// "/settings" starts with "/s" but is not a share path — a prefix
		// check on "/s" rather than "/s/" would have mangled it.
		"/settings",
		"/static/app.js",
	} {
		u, _ := url.Parse(in)
		if got := redactURI(u); got != "" {
			t.Errorf("%s was rewritten to %q", in, got)
		}
	}
}

// The regression that matters, and the one a unit test on redactURI alone
// would have missed: chi routes off r.URL.Path. An earlier version of this
// middleware rewrote the URL as well as the request line, so the router
// matched "/s/REDACTED" and `{token}` reached the handler as the literal
// string "REDACTED" — every share link answered 404 while the log looked
// perfect.
func TestRedactingLoggerDoesNotBreakRouting(t *testing.T) {
	var seen string
	r := chi.NewRouter()
	r.Use(redactingLogger)
	r.Get("/s/{token}", func(w http.ResponseWriter, req *http.Request) {
		seen = chi.URLParam(req, "token")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/s/the-real-token", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if seen != "the-real-token" {
		t.Errorf("handler saw token %q, want the real one — the router matched a redacted path", seen)
	}
}

// And the handler must still be able to read a redacted query parameter,
// because it is the credential the request is authorized by.
func TestRedactingLoggerLeavesTheQueryReadable(t *testing.T) {
	var seen string
	r := chi.NewRouter()
	r.Use(redactingLogger)
	r.Get("/wopi/files/{id}/contents", func(w http.ResponseWriter, req *http.Request) {
		seen = req.URL.Query().Get("access_token")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/wopi/files/n1/contents?access_token=real-token", nil))

	if seen != "real-token" {
		t.Errorf("handler saw access_token %q, want the real one", seen)
	}
}
