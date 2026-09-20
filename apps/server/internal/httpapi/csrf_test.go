package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func originServer() *Server {
	return &Server{externalURL: "https://drive.example.com"}
}

func originRequest(t *testing.T, method, origin, referer string) *httptest.ResponseRecorder {
	t.Helper()
	reached := false
	h := originServer().sameOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(method, "/api/v1/nodes/x/star", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if reached != (rec.Code == http.StatusNoContent) {
		t.Fatalf("handler reached=%v but status=%d", reached, rec.Code)
	}
	return rec
}

func TestSameOriginAcceptsOwnOrigin(t *testing.T) {
	if rec := originRequest(t, "POST", "https://drive.example.com", ""); rec.Code != http.StatusNoContent {
		t.Errorf("own origin refused: %d %s", rec.Code, rec.Body)
	}
}

func TestSameOriginRefusesForeignOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://evil.example",
		// A prefix of the real host is not the real host.
		"https://drive.example.com.evil.example",
		// Nor is the same host on another scheme: an http origin on an https
		// deployment is a downgrade, not a same-origin request.
		"http://drive.example.com",
		// Nor another port.
		"https://drive.example.com:8443",
		"null",
	} {
		if rec := originRequest(t, "POST", origin, ""); rec.Code != http.StatusForbidden {
			t.Errorf("Origin %q accepted with %d", origin, rec.Code)
		}
	}
}

// Referer is the fallback for clients that omit Origin, and only its origin
// may be compared — the path is attacker-controlled, so a naive prefix match
// on the whole URL would accept "https://evil.example/?x=https://drive…".
func TestSameOriginFallsBackToRefererOrigin(t *testing.T) {
	if rec := originRequest(t, "POST", "", "https://drive.example.com/drive/abc?dir=x"); rec.Code != http.StatusNoContent {
		t.Errorf("same-origin Referer refused: %d", rec.Code)
	}
	if rec := originRequest(t, "POST", "", "https://evil.example/https://drive.example.com"); rec.Code != http.StatusForbidden {
		t.Errorf("foreign Referer accepted: %d", rec.Code)
	}
}

// Neither header is not a pass. A cookie-authenticated write with nothing to
// verify is the case this middleware exists for.
func TestSameOriginRefusesWhenBothHeadersAbsent(t *testing.T) {
	if rec := originRequest(t, "POST", "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("headerless write accepted: %d", rec.Code)
	}
}

// Safe methods pass untouched, or a plain <a href> download would break.
func TestSameOriginIgnoresSafeMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		if rec := originRequest(t, method, "", ""); rec.Code != http.StatusNoContent {
			t.Errorf("%s refused: %d", method, rec.Code)
		}
	}
}

// Every unsafe method, not just POST — a PATCH or DELETE from another origin
// is the same attack.
func TestSameOriginCoversEveryUnsafeMethod(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if rec := originRequest(t, method, "https://evil.example", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s from a foreign origin accepted: %d", method, rec.Code)
		}
	}
}

// A deployment whose PP_EXTERNAL_URL is unusable must refuse writes rather
// than wave them through: "nothing to compare against" is the one failure
// mode that would silently disable the control everywhere.
func TestSameOriginFailsClosedWithoutAnExternalURL(t *testing.T) {
	h := (&Server{externalURL: "not a url"}).sameOrigin(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest("POST", "/api/v1/spaces", nil)
	req.Header.Set("Origin", "https://drive.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("write allowed with no origin to compare against: %d", rec.Code)
	}
}
