package httpapi

import (
	"net/http"
	"regexp"
	"strings"
)

// guestRoutes is every /api/v1 route a guest session may call: read what was
// shared, and take part in the discussion of it (guest-access.md). Patterns
// are the router's own, as chi.Walk reports them without the /api/v1 prefix,
// so TestGuestRoutesAreRoutes can hold the list to the table.
//
// It is an allowlist for the reason authentication is a middleware: a route
// added next year is closed to guests until somebody decides otherwise here.
// Whether a guest may use a route on a particular node is still the
// capability resolver's decision, as for anyone.
var guestRoutes = []string{
	"GET /users/me",
	"GET /users/{userID}/avatar",
	"GET /spaces/",
	"GET /shared",
	"GET /nodes/{nodeID}/",
	"GET /nodes/{nodeID}/children",
	"GET /nodes/{nodeID}/path",
	"GET /nodes/{nodeID}/people",
	"GET /nodes/{nodeID}/download",
	"GET /nodes/{nodeID}/content",
	"GET /nodes/{nodeID}/preview",
	"GET /nodes/{nodeID}/document",
	"GET /nodes/{nodeID}/attachments/{hash}",
	"GET /nodes/{nodeID}/relative",
	"GET /nodes/{nodeID}/relative/meta",
	"GET /nodes/{nodeID}/page",
	"GET /nodes/{nodeID}/pages",
	"GET /nodes/{nodeID}/backlinks",
	"GET /nodes/{nodeID}/export",
	"GET /nodes/{nodeID}/collab",
	"POST /nodes/{nodeID}/edit-session",
	"GET /nodes/{nodeID}/comments",
	"POST /nodes/{nodeID}/comment-threads",
	"POST /comment-threads/{threadID}/comments",
	"POST /comment-threads/{threadID}/resolve",
	"POST /comment-threads/{threadID}/reopen",
	"PATCH /comments/{commentID}",
	"DELETE /comments/{commentID}",
	"GET /collab/{room}",
	"GET /notifications",
	"GET /notifications/unread-count",
	"POST /notifications/read",
	"GET /notifications/stream",
}

type guestRoute struct {
	method string
	re     *regexp.Regexp
}

var guestMatchers = func() []guestRoute {
	out := make([]guestRoute, 0, len(guestRoutes))
	for _, spec := range guestRoutes {
		method, pattern, _ := strings.Cut(spec, " ")
		out = append(out, guestRoute{method: method, re: guestPattern(pattern)})
	}
	return out
}()

var paramSegment = regexp.MustCompile(`\{[^/}]+\}`)

// guestPattern turns a chi pattern into an anchored expression in which a
// parameter is exactly one path segment.
func guestPattern(pattern string) *regexp.Regexp {
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		pattern = "/"
	}
	parts := paramSegment.Split(pattern, -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	quoted := strings.Join(parts, `[^/]+`)
	return regexp.MustCompile(`^` + quoted + `$`)
}

// guestMayCall reports whether a guest session may make this request.
//
// It reads the path the way chi routes it — the raw form when there is one —
// so a path that decodes into an allowed shape but routes elsewhere cannot
// pass: the two agree or the match fails and the guest is refused.
func guestMayCall(r *http.Request) bool {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	path = strings.TrimPrefix(path, "/api/v1")
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	for _, m := range guestMatchers {
		if m.method == r.Method && m.re.MatchString(path) {
			return true
		}
	}
	return false
}
