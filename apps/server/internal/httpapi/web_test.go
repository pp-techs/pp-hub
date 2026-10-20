package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestWeb(t *testing.T) *WebApp {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"index.html":    "<!doctype html><title>shell</title>",
		"assets/app.js": "console.log(1)",
		"favicon.ico":   "ico",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app, err := NewWebApp(dir, "https://office.example", "https://hub.example", "")
	if err != nil {
		t.Fatalf("NewWebApp: %v", err)
	}
	return app
}

func webGet(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = "hub.example"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewWebAppRefusesADirectoryWithoutIndex(t *testing.T) {
	if _, err := NewWebApp(t.TempDir(), "", "", ""); err == nil {
		t.Fatal("a directory with no index.html was accepted")
	}
}

func TestWebServesBundleAndFallsBackForClientRoutes(t *testing.T) {
	h := (&Server{web: newTestWeb(t)}).Router()

	for _, tc := range []struct {
		name, path   string
		status       int
		body         string
		cacheControl string
	}{
		{"root", "/", 200, "shell", "no-cache, must-revalidate"},
		{"client route", "/drive/folder/abc", 200, "shell", "no-cache, must-revalidate"},
		{"index itself", "/index.html", 200, "shell", "no-cache, must-revalidate"},
		{"hashed asset", "/assets/app.js", 200, "console.log(1)", "public, max-age=31536000, immutable"},
		{"missing asset is not a route", "/assets/gone.js", 404, "", ""},
		{"top-level file", "/favicon.ico", 200, "ico", ""},
		{"traversal stays inside the bundle", "/../../etc/passwd", 200, "shell", "no-cache, must-revalidate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := webGet(h, http.MethodGet, tc.path, nil)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d", rec.Code, tc.status)
			}
			if !strings.Contains(rec.Body.String(), tc.body) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.body)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.cacheControl {
				t.Errorf("Cache-Control %q, want %q", got, tc.cacheControl)
			}
		})
	}
}

// A mistyped API path answering 200 with HTML is the failure nobody notices:
// the client gets a body that is not JSON and reports a parse error three
// layers away.
func TestWebNeverAnswersForPathsTheAPIOwns(t *testing.T) {
	h := (&Server{web: newTestWeb(t)}).Router()
	for _, p := range []string{"/hooks/nope", "/wopi/files/x", "/dav-not/x"} {
		rec := webGet(h, http.MethodGet, p, nil)
		wantShell := p == "/dav-not/x" // a prefix of a word, not a path segment
		if got := strings.Contains(rec.Body.String(), "shell"); got != wantShell {
			t.Errorf("GET %s: app shell served = %v, want %v (status %d)", p, got, wantShell, rec.Code)
		}
		if !wantShell && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, rec.Code)
		}
	}
}

func TestWebRefusesWrites(t *testing.T) {
	h := (&Server{web: newTestWeb(t)}).Router()
	rec := webGet(h, http.MethodPost, "/drive", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to a client route: %d, want 405", rec.Code)
	}
}

// GET /s/<token> is the API's JSON and the SPA's page at once; only the
// bare token with a browser's Accept is the page.
func TestShareLandingSplitsOnAccept(t *testing.T) {
	app := newTestWeb(t)
	reachedAPI := false
	h := app.shareLanding(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reachedAPI = true
		w.WriteHeader(http.StatusTeapot)
	}))
	const browser = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	for _, tc := range []struct {
		name, method, path, accept string
		shell                      bool
	}{
		{"navigation", http.MethodGet, "/s/abc123", browser, true},
		{"spa fetch", http.MethodGet, "/s/abc123", "application/json", false},
		{"no accept", http.MethodGet, "/s/abc123", "", false},
		{"sub-path is always the API", http.MethodGet, "/s/abc123/download", browser, false},
		{"trailing slash is the API", http.MethodGet, "/s/abc123/", browser, false},
		{"bare /s/", http.MethodGet, "/s/", browser, false},
		{"not a read", http.MethodPost, "/s/abc123", browser, false},
		{"another path", http.MethodGet, "/settings", browser, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reachedAPI = false
			hdr := map[string]string{}
			if tc.accept != "" {
				hdr["Accept"] = tc.accept
			}
			rec := webGet(h, tc.method, tc.path, hdr)
			if tc.shell {
				if reachedAPI || !strings.Contains(rec.Body.String(), "shell") {
					t.Fatalf("want the app shell, got api=%v body=%q", reachedAPI, rec.Body.String())
				}
				return
			}
			if !reachedAPI {
				t.Fatalf("want the API, got body %q", rec.Body.String())
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := (&Server{web: newTestWeb(t)}).Router()
	rec := webGet(h, http.MethodGet, "/", nil)

	for k, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"frame-src 'self' https://office.example;",
		"form-action 'self' https://office.example;",
		"connect-src 'self' wss://hub.example ws://hub.example",
		"object-src 'self';",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}

	// The same headers ride on an API-side response too, as they did behind nginx.
	if rec := webGet(h, http.MethodGet, "/healthz", nil); rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("/healthz carries no CSP")
	}
}

func TestHSTSOnlyForHTTPSDeployments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewWebApp(dir, "http://localhost:9980", "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := webGet((&Server{web: app}).Router(), http.MethodGet, "/", nil)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS sent for an http deployment: %q", got)
	}
}

// Host is the caller's to choose and lands in a header that decides what the
// page may load. One that could end a directive must not reach the policy.
func TestCSPDoesNotSpliceAHostileHost(t *testing.T) {
	app := newTestWeb(t)
	for _, host := range []string{
		"evil.example; script-src *",
		"a b",
		"x.example,y",
		"",
	} {
		csp := app.csp(host)
		if strings.Contains(csp, "evil") || strings.Contains(csp, "wss://") {
			t.Errorf("host %q reached the policy: %s", host, csp)
		}
	}
	for _, host := range []string{"hub.example", "hub.example:8443", "localhost:5173", "[::1]:8080", "10.0.0.5"} {
		if csp := app.csp(host); !strings.Contains(csp, "wss://"+host+" ") {
			t.Errorf("host %q was dropped from the policy", host)
		}
	}
}
