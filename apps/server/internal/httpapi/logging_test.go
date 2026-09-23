package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
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
func TestObserveDoesNotBreakRouting(t *testing.T) {
	var seen string
	r := chi.NewRouter()
	r.Use((&Server{}).observe)
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
func TestObserveLeavesTheQueryReadable(t *testing.T) {
	var seen string
	r := chi.NewRouter()
	r.Use((&Server{}).observe)
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

// captureLog routes the default logger into a JSON buffer for one test and
// returns the decoded records on demand.
func captureLog(t *testing.T) (*bytes.Buffer, func() []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(obs.NewHandler(&buf, slog.LevelDebug, "json")))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf, func() []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("log line is not JSON: %q", line)
			}
			out = append(out, rec)
		}
		return out
	}
}

// The access log line carries the redacted path, the route pattern and the
// same request id the caller got back — that id is what joins a user report
// to the server's logs.
func TestAccessLogRedactsAndCorrelates(t *testing.T) {
	buf, records := captureLog(t)
	r := chi.NewRouter()
	r.Use(middleware.RequestID, (&Server{}).observe)
	r.Get("/s/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/s/secret-token", nil))

	reqID := rec.Header().Get("X-Request-Id")
	if reqID == "" {
		t.Fatal("response has no X-Request-Id")
	}
	var lines []map[string]any
	for _, l := range records() {
		if l["msg"] == "http request" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("got %d access log lines, want 1", len(lines))
	}
	l := lines[0]
	if l["path"] != "/s/REDACTED" || l["route"] != "/s/{token}" || l["status"] != float64(204) {
		t.Errorf("unexpected access log line: %v", l)
	}
	if l["request_id"] != reqID {
		t.Errorf("request_id %v, want the response header %q", l["request_id"], reqID)
	}
	if strings.Contains(buf.String(), "secret-token") {
		t.Error("the share token reached the log")
	}
}

// The route label must be the pattern: a raw path would make every node id
// (and every share token) a new series. That includes requests a subrouter
// middleware answers before routing finishes — an unauthenticated call must
// be counted under its endpoint, not under the mount prefix.
func TestMetricsUseRoutePattern(t *testing.T) {
	captureLog(t)
	reg := prometheus.NewRegistry()
	s := &Server{metrics: obs.NewHTTPMetrics(reg)}
	r := chi.NewRouter()
	r.Use(s.observe)
	r.Get("/nodes/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	r.Route("/api", func(r chi.Router) {
		r.Use(func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			})
		})
		r.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	})

	for _, p := range []string{"/nodes/a", "/nodes/b", "/nope", "/api/items/x"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}

	want := `
# HELP pphub_http_requests_total HTTP requests served, by method, chi route pattern and status.
# TYPE pphub_http_requests_total counter
pphub_http_requests_total{method="GET",route="/api/items/{id}",status="401"} 1
pphub_http_requests_total{method="GET",route="/nodes/{id}",status="200"} 2
pphub_http_requests_total{method="GET",route="unmatched",status="404"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "pphub_http_requests_total"); err != nil {
		t.Error(err)
	}
	// Every Start was matched by a Done.
	inFlight := `
# HELP pphub_http_requests_in_flight HTTP requests currently being served.
# TYPE pphub_http_requests_in_flight gauge
pphub_http_requests_in_flight 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(inFlight), "pphub_http_requests_in_flight"); err != nil {
		t.Error(err)
	}
}

// A panicking handler answers a problem+json 500 and is logged at ERROR with
// the request id, instead of a bare stack on stderr.
func TestRecovererAnswers500(t *testing.T) {
	_, records := captureLog(t)
	r := chi.NewRouter()
	r.Use(middleware.RequestID, (&Server{}).observe, recoverer)
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content type %q", ct)
	}
	var found bool
	for _, l := range records() {
		if l["msg"] == "panic" {
			found = true
			if l["level"] != "ERROR" || l["request_id"] == nil || l["request_id"] == "" {
				t.Errorf("panic record %v lacks ERROR level or request_id", l)
			}
		}
	}
	if !found {
		t.Error("the panic was not logged")
	}
}
