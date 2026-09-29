package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin rejects a state-changing request whose Origin is not this
// deployment.
//
// `SameSite=Lax` on the session cookie already withholds it from cross-site
// POSTs, which is most of the defence — but it is one browser's policy, it
// does not cover a top-level form navigation, and it is exactly the kind of
// single control that should not be the only one. This is the second: a
// cross-origin caller cannot forge an `Origin` header, so comparing it to the
// one origin this server is reachable at closes the gap for every unsafe
// method.
//
// It deliberately wraps only `/api/v1` and `/auth/logout`. The two other
// mutating surfaces are cross-origin by design and authorized differently:
// `/wopi/*` is the Document Server POSTing with a signed access token and no
// Origin at all, and `/auth/callback` is a top-level navigation from the IdP
// whose defence is the state and nonce in `pp_oidc_tx`.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	want := OriginOf(s.externalURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Origin")
		if got == "" {
			// Older browsers omit Origin on same-origin requests. Referer is
			// the fallback, and its origin is compared the same way — never
			// its path, which the caller controls.
			got = OriginOf(r.Header.Get("Referer"))
		}
		if refusal := originRefusal(want, got); refusal != "" {
			writeProblem(w, http.StatusForbidden, "Forbidden", refusal)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// socketOrigin is sameOrigin for the one GET that needs it: a WebSocket
// upgrade. The upgrade carries the session cookie and hands the caller a
// socket that reads and writes a document, so a page on another origin must
// not be able to open one — and sameOrigin lets every GET through.
//
// It is stricter than sameOrigin in one way: Origin is required, with no
// Referer fallback. Browsers always send Origin on an upgrade, so a request
// without one is not a browser acting for a signed-in user. And it runs before
// ygo sees the request, because ygo authorizes — and loads the room — before
// its own origin check, and admits a request with no Origin at all.
func (s *Server) socketOrigin(next http.Handler) http.Handler {
	want := OriginOf(s.externalURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if refusal := originRefusal(want, r.Header.Get("Origin")); refusal != "" {
			writeProblem(w, http.StatusForbidden, "Forbidden", refusal)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originRefusal compares the origin a request claims with this deployment's
// and returns why it is refused, or "" when it is this deployment.
func originRefusal(want, got string) string {
	switch {
	case want == "":
		// A misconfigured PP_EXTERNAL_URL must not turn into an open door, so
		// an unparseable one refuses everything rather than allowing it.
		return "this server has no valid PP_EXTERNAL_URL to compare the request origin against"
	case got == "":
		// Nothing to verify: not a browser, or a browser deliberately
		// stripping the headers. A cookie-authenticated request is not
		// something to guess about.
		return "missing Origin"
	case !strings.EqualFold(got, want):
		return "cross-origin request refused"
	}
	return ""
}

// OriginOf reduces a URL to scheme://host, the only part of it that is not
// attacker-controlled once the browser has set the header — and the form
// PP_EXTERNAL_URL is compared in.
func OriginOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
