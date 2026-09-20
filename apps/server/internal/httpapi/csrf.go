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
	want := originOf(s.externalURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// A misconfigured PP_EXTERNAL_URL must not turn into an open door, so
		// an unparseable one refuses everything rather than allowing it.
		if want == "" {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"this server has no valid PP_EXTERNAL_URL to compare the request origin against")
			return
		}
		got := r.Header.Get("Origin")
		if got == "" {
			// Older browsers omit Origin on same-origin requests. Referer is
			// the fallback, and its origin is compared the same way — never
			// its path, which the caller controls.
			got = originOf(r.Header.Get("Referer"))
		}
		if got == "" {
			// Neither header: not a browser, or a browser deliberately
			// stripping them. Either way there is nothing to verify, and a
			// cookie-authenticated write is not something to guess about.
			writeProblem(w, http.StatusForbidden, "Forbidden", "missing Origin")
			return
		}
		if !strings.EqualFold(got, want) {
			writeProblem(w, http.StatusForbidden, "Forbidden", "cross-origin request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originOf reduces a URL to scheme://host, the only part of it that is not
// attacker-controlled once the browser has set the header.
func originOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
