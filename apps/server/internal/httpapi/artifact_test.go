package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
)

// The artifact host serves published bytes and nothing else, and the main
// host never serves an artifact path. Uploaded HTML on the session's origin
// would be stored XSS; the API on the artifact origin would be the session
// reachable from it. No database: the switch is off, so every artifact
// path is a 404 — what is asserted is who answers.
func TestArtifactHostServesNothingElse(t *testing.T) {
	web := newTestWeb(t)
	srv := &Server{
		externalURL:  "https://drive.example",
		web:          web,
		artifactHost: "art.example",
		publish: publish.New(publish.Config{
			ArtifactURL: "https://art.example", Enabled: func() bool { return false },
		}),
	}
	h := srv.Router()

	send := func(host, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// The main router's surfaces, asked of the artifact host.
	for _, path := range []string{
		"/", "/healthz", "/api/v1/users/me", "/auth/login", "/mcp/", "/dav/",
		"/s/sometoken1234567", "/p/sometoken1234567", "/drive/x", "/index.html",
	} {
		rec := send("art.example", http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("artifact host GET %s = %d, want 404", path, rec.Code)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.HasPrefix(csp, "sandbox allow-scripts ") || !strings.Contains(csp, "frame-ancestors https://drive.example") {
			t.Errorf("artifact host GET %s: CSP %q is not the sandbox", path, csp)
		}
		if rec.Header().Get("X-Frame-Options") != "" || strings.Contains(rec.Body.String(), "shell") {
			t.Errorf("artifact host GET %s answered like the main site", path)
		}
	}
	if rec := send("art.example", http.MethodPost, "/api/v1/spaces"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("artifact host POST = %d, want 405", rec.Code)
	}
	if rec := send("ART.EXAMPLE", http.MethodGet, "/healthz"); rec.Code != http.StatusNotFound {
		t.Errorf("artifact host matched case-sensitively: %d", rec.Code)
	}

	// An artifact path asked of the main host is any unmatched path: a
	// client-side route or a 404, never published bytes, always the main
	// site's own headers. With an artifact origin configured, /a/ is not
	// served here either.
	for _, path := range []string{"/sometoken1234567/index.html", "/a/sometoken1234567/index.html"} {
		rec := send("drive.example", http.MethodGet, path)
		if strings.HasPrefix(rec.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("main host answered %s with the artifact sandbox", path)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("main host lost its own security headers on %s", path)
		}
	}
	if rec := send("drive.example", http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("main host /healthz = %d", rec.Code)
	}
}

// The viewer frames the artifact origin, so the main CSP names it.
func TestMainCSPFramesTheArtifactOrigin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewWebApp(dir, "", "https://drive.example", "https://art.example")
	if err != nil {
		t.Fatal(err)
	}
	csp := app.csp("drive.example")
	if !strings.Contains(csp, "frame-src 'self' https://art.example;") {
		t.Fatalf("frame-src does not name the artifact origin: %s", csp)
	}
	if strings.Contains(csp, "form-action 'self' https://art.example") {
		t.Fatalf("the artifact origin became a form target: %s", csp)
	}
}

func TestRedactArtifactURI(t *testing.T) {
	for in, want := range map[string]string{
		"/tok123/index.html":       "/REDACTED/index.html",
		"/tok123/~grantabc/app.js": "/REDACTED/~REDACTED/app.js",
		"/tok123":                  "/REDACTED",
		"/tok123/~grantabc":        "/REDACTED/~REDACTED",
		"/tok123/a.js?x=tok123":    "/REDACTED/a.js",
	} {
		u, _ := url.Parse(in)
		if got := redactArtifactURI(u); got != want {
			t.Errorf("redactArtifactURI(%q) = %q, want %q", in, got, want)
		}
	}
	u, _ := url.Parse("/p/secrettoken/unlock")
	if got := redactURI(u); got != "/p/REDACTED/unlock" {
		t.Errorf("redactURI(/p/...) = %q", got)
	}
}
