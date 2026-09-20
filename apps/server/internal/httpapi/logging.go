package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

// redactingLogger is chi's request logger with the two bearer credentials
// this API carries in URLs removed from the log line.
//
// A share token *is* the authorization for anonymous access to a file, and it
// is a path segment — so the plain request line put it in stdout, and from
// there into whatever ships container logs, a log-search index or a support
// bundle. Only its SHA-256 is stored in the database; logging the plaintext
// next to it made that decision pointless. The WOPI access token is the same
// problem in a query parameter.
//
// It rewrites `r.RequestURI` and nothing else. That is deliberate and it is
// the whole subtlety here: `RequestURI` is the raw string chi's Logger
// formats, while routing runs off `r.URL.Path`. An earlier version swapped
// the URL too — and chi then matched the *redacted* path, so `{token}`
// resolved to "REDACTED" and every share link answered 404. Leave `r.URL`
// alone.
func redactingLogger(next http.Handler) http.Handler {
	logger := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redacted := redactURI(r.URL); redacted != "" {
			r.RequestURI = redacted
		}
		logger.ServeHTTP(w, r)
	})
}

// redactURI returns the request URI with secrets replaced, or "" when there
// is nothing to hide — the overwhelmingly common case, which costs one prefix
// check and one map lookup.
func redactURI(u *url.URL) string {
	isShare := strings.HasPrefix(u.Path, "/s/")
	hasToken := u.Query().Get("access_token") != ""
	if !isShare && !hasToken {
		return ""
	}

	path := u.Path
	if isShare {
		// "/s/<token>/download" → "/s/REDACTED/download". The shape of the
		// request stays readable, which is the only reason to log it.
		rest := strings.TrimPrefix(path, "/s/")
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			path = "/s/REDACTED" + rest[i:]
		} else {
			path = "/s/REDACTED"
		}
	}

	query := u.RawQuery
	if hasToken {
		q := u.Query()
		q.Set("access_token", "REDACTED")
		query = q.Encode()
	}
	if query != "" {
		return path + "?" + query
	}
	return path
}
