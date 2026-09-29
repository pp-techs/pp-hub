package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
)

// requestLog is what inner handlers contribute to the access log line; for
// now the authenticated user, filled in by `authenticated`.
type requestLog struct{ userID string }

// observe writes the access log line and feeds the HTTP metrics — one
// ResponseWriter wrap for both. The request id reaches the line through the
// context (obs's handler adds it) and the caller through X-Request-Id, so a
// user-reported failure can be matched to log lines and security_events rows.
//
// The two bearer credentials this API carries in URLs are removed from the
// logged path. A share token *is* the authorization for anonymous access to a
// file, and it is a path segment — logging it would put it in whatever ships
// container logs, while only its SHA-256 is stored in the database. The WOPI
// access token is the same problem in a query parameter.
//
// The request itself is never rewritten. An earlier version swapped r.URL
// for the redacted one and chi then matched the *redacted* path, so `{token}`
// resolved to "REDACTED" and every share link answered 404. Metrics use the
// chi route pattern, which contains no token either.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.metrics.Start()
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		rl := &requestLog{}
		r = r.WithContext(context.WithValue(r.Context(), logCtxKey, rl))
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				// The connection was hijacked: the upgrade wrote its 101
				// straight to the socket, past this writer, and the line is
				// logged when the socket closes.
				status = http.StatusSwitchingProtocols
			}
		}
		elapsed := time.Since(start)
		route := routePattern(r)
		path := redactURI(r.URL)
		if path == "" {
			path = r.URL.RequestURI()
		}

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case route == "/healthz" || route == "/readyz":
			// Compose probes these every ten seconds.
			level = slog.LevelDebug
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", path),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000),
			slog.String("remote", audit.ClientIP(r)),
		}
		if rl.userID != "" {
			attrs = append(attrs, slog.String("user_id", rl.userID))
		}
		slog.LogAttrs(r.Context(), level, "http request", attrs...)
		s.metrics.Done(r.Method, route, status, elapsed)
	})
}

// routePattern is the chi pattern the request matched, or "unmatched". A
// subrouter middleware that answers before routing finishes (authenticated's
// 401, a rate limit's 429) leaves only the mount prefix, "/api/v1/*"; the
// full pattern is then looked up in the root router without running anything.
func routePattern(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return "unmatched"
	}
	pattern := rc.RoutePattern()
	if strings.HasSuffix(pattern, "/*") && rc.Routes != nil {
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		if full := rc.Routes.Find(chi.NewRouteContext(), r.Method, path); full != "" {
			pattern = full
		}
	}
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}

// recoverer replaces chi's, which prints a text stack straight to stderr and
// so bypasses the structured log. It runs inside observe, so a panic is
// logged and counted as the 500 it becomes.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "panic", "panic", rec, "stack", string(debug.Stack()))
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		}()
		next.ServeHTTP(w, r)
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
