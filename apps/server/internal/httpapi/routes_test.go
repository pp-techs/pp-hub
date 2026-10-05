package httpapi

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Every route the web client calls, as METHOD + pattern.
//
// This list exists because DELETE /nodes/{nodeID}/shares/{shareID} was written,
// tested by hand, and then never registered — handleRevokeShare sat unreachable
// while the UI's revoke button returned 404. A handler that no router mentions
// is invisible to the compiler, so the router itself has to be asserted.
var clientRoutes = []string{
	"GET /api/v1/users/me",
	"PUT /api/v1/users/me",
	"POST /api/v1/users/me/avatar",
	"GET /api/v1/users/me/sessions/",
	"DELETE /api/v1/users/me/sessions/",
	"DELETE /api/v1/users/me/sessions/{sessionID}",
	"GET /api/v1/users/me/agent-tokens/",
	"POST /api/v1/users/me/agent-tokens/",
	"DELETE /api/v1/users/me/agent-tokens/{tokenID}",
	"GET /api/v1/users",
	"GET /api/v1/admin/stats",
	"GET /api/v1/admin/security-events",
	"GET /api/v1/admin/security-events/export",
	"PATCH /api/v1/admin/users/{userID}",
	"GET /api/v1/admin/settings",
	"PATCH /api/v1/admin/settings",
	"DELETE /api/v1/admin/settings/{key}",
	"POST /api/v1/admin/settings/smtp/test",
	"GET /api/v1/users/{userID}/avatar",
	"GET /api/v1/search",
	"GET /api/v1/activity",
	"GET /api/v1/recent",
	"GET /api/v1/starred",
	"GET /api/v1/usage",
	"GET /api/v1/shared",

	"POST /api/v1/spaces/",
	"GET /api/v1/spaces/",
	"PATCH /api/v1/spaces/{spaceID}/",
	"DELETE /api/v1/spaces/{spaceID}/",

	// A space's picture: stored in the CAS, served to its members.
	"GET /api/v1/spaces/{spaceID}/avatar",
	"POST /api/v1/spaces/{spaceID}/avatar",
	"DELETE /api/v1/spaces/{spaceID}/avatar",

	// Groups and the grants that give them access to folders.
	"GET /api/v1/spaces/{spaceID}/groups/",
	"POST /api/v1/spaces/{spaceID}/groups/",
	"PATCH /api/v1/spaces/{spaceID}/groups/{groupID}/",
	"DELETE /api/v1/spaces/{spaceID}/groups/{groupID}/",
	"GET /api/v1/spaces/{spaceID}/groups/{groupID}/members",
	"POST /api/v1/spaces/{spaceID}/groups/{groupID}/members",
	"DELETE /api/v1/spaces/{spaceID}/groups/{groupID}/members/{userID}",
	"GET /api/v1/nodes/{nodeID}/grants",
	"PUT /api/v1/nodes/{nodeID}/grants",
	"DELETE /api/v1/nodes/{nodeID}/grants/{groupID}",
	"GET /api/v1/spaces/{spaceID}/children",
	"GET /api/v1/spaces/{spaceID}/tree",
	"GET /api/v1/spaces/{spaceID}/trash",
	"POST /api/v1/spaces/{spaceID}/trash/empty",
	"POST /api/v1/spaces/{spaceID}/dirs",
	"POST /api/v1/spaces/{spaceID}/documents",

	// Resumable uploads — the only way file content comes in.
	"POST /api/v1/uploads/",
	"HEAD /api/v1/uploads/{uploadID}/",
	"PATCH /api/v1/uploads/{uploadID}/",
	"DELETE /api/v1/uploads/{uploadID}/",

	"GET /api/v1/spaces/{spaceID}/members/",
	"POST /api/v1/spaces/{spaceID}/members/",
	"POST /api/v1/spaces/{spaceID}/members/{userID}/set-admin",
	"DELETE /api/v1/spaces/{spaceID}/members/{userID}/",
	"GET /api/v1/spaces/{spaceID}/departures",
	"POST /api/v1/spaces/{spaceID}/departures/{userID}/complete",

	"GET /api/v1/nodes/{nodeID}/",
	"GET /api/v1/nodes/{nodeID}/download",
	"GET /api/v1/nodes/{nodeID}/content",
	"GET /api/v1/nodes/{nodeID}/preview",
	"PATCH /api/v1/nodes/{nodeID}/rename",
	"PATCH /api/v1/nodes/{nodeID}/move",
	"POST /api/v1/nodes/{nodeID}/copy",
	"DELETE /api/v1/nodes/{nodeID}/",
	"POST /api/v1/nodes/{nodeID}/star",
	"DELETE /api/v1/nodes/{nodeID}/star",
	"POST /api/v1/nodes/{nodeID}/restore",
	"DELETE /api/v1/nodes/{nodeID}/purge",
	"GET /api/v1/nodes/{nodeID}/versions",
	"POST /api/v1/nodes/{nodeID}/versions/{versionID}/restore",
	"POST /api/v1/nodes/{nodeID}/shares",
	"GET /api/v1/nodes/{nodeID}/shares",
	"DELETE /api/v1/nodes/{nodeID}/shares/{shareID}",
	"GET /api/v1/nodes/{nodeID}/inherited-access",
	"POST /api/v1/nodes/{nodeID}/edit-session",

	// Block documents: the room, its socket, and the document's attachments.
	"GET /api/v1/nodes/{nodeID}/collab",
	"GET /api/v1/collab/{room}",
	"GET /api/v1/nodes/{nodeID}/attachments/{hash}",
	"GET /api/v1/nodes/{nodeID}/document",
	"GET /api/v1/nodes/{nodeID}/export",
	"POST /api/v1/nodes/{nodeID}/unfurl",
	"GET /api/v1/templates/markdown",
	"GET /s/{token}/document",
	"GET /s/{token}/attachments/{hash}",

	// Pages (M43): the page tree, breadcrumb and backlinks, signed in and
	// through a link.
	"GET /api/v1/nodes/{nodeID}/page",
	"GET /api/v1/nodes/{nodeID}/pages",
	"GET /api/v1/nodes/{nodeID}/backlinks",
	"GET /s/{token}/page",
	"GET /s/{token}/pages",

	// Comments on block documents, and in-app notifications (M29).
	"GET /api/v1/nodes/{nodeID}/comments",
	"POST /api/v1/nodes/{nodeID}/comment-threads",
	"POST /api/v1/comment-threads/{threadID}/comments",
	"POST /api/v1/comment-threads/{threadID}/resolve",
	"POST /api/v1/comment-threads/{threadID}/reopen",
	"PATCH /api/v1/comments/{commentID}",
	"DELETE /api/v1/comments/{commentID}",
	"GET /api/v1/notifications",
	"GET /api/v1/notifications/unread-count",
	"POST /api/v1/notifications/read",
	"GET /api/v1/notifications/stream",

	"GET /s/{token}/",
	"GET /s/{token}/download",
	"GET /s/{token}/children",
	"GET /s/{token}/content",
	"GET /s/{token}/node-download",
	"GET /s/{token}/download-all",
	"GET /s/{token}/edit-session",
}

func TestRouterRegistersClientRoutes(t *testing.T) {
	// Router only takes method values off the service fields; it never calls
	// them, so a zero Server is enough to build the routing table.
	registered := map[string]bool{}
	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			registered[method+" "+route] = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	var missing []string
	for _, want := range clientRoutes {
		if !registered[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("routes not registered:\n  %s", strings.Join(missing, "\n  "))
	}
}

// Authentication is a middleware on the /api/v1 group, not a line at the top
// of each handler. That is the only reason a new route cannot ship public by
// omission — so the wiring that makes it true is asserted, not assumed.
//
// The check is on the middleware chain rather than on a live 401, because a
// zero Server has no session store to answer with and the invariant being
// defended is structural: this handler is wrapped, whatever it does.
func TestEveryAPIRouteIsAuthenticated(t *testing.T) {
	guard := reflect.ValueOf((&Server{}).authenticated).Pointer()

	var unguarded []string
	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			if !strings.HasPrefix(route, "/api/v1/") {
				return nil
			}
			for _, mw := range middlewares {
				if reflect.ValueOf(mw).Pointer() == guard {
					return nil
				}
			}
			unguarded = append(unguarded, method+" "+route)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(unguarded) > 0 {
		sort.Strings(unguarded)
		t.Errorf("routes reachable without authentication:\n  %s", strings.Join(unguarded, "\n  "))
	}
}

// The other half of the pair. `SameSite=Lax` is a browser policy, not this
// server's control, so every state-changing route also compares Origin — and
// like authentication, it is a middleware rather than seventy chances to
// forget.
//
// GET routes are excluded on purpose: they change nothing, and demanding
// Origin on them would break a plain `<a href>` into a download.
func TestEveryAPIMutationChecksOrigin(t *testing.T) {
	guard := reflect.ValueOf((&Server{}).sameOrigin).Pointer()

	var unguarded []string
	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			if !strings.HasPrefix(route, "/api/v1/") {
				return nil
			}
			switch method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return nil
			}
			for _, mw := range middlewares {
				if reflect.ValueOf(mw).Pointer() == guard {
					return nil
				}
			}
			unguarded = append(unguarded, method+" "+route)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(unguarded) > 0 {
		sort.Strings(unguarded)
		t.Errorf("mutations reachable cross-origin:\n  %s", strings.Join(unguarded, "\n  "))
	}
}

// The WOPI group must NOT be wrapped. The Document Server POSTs saves with no
// Origin header at all, so a check there would break every office save — and
// the reason it is safe is that those routes are authorized by a signed
// access token rather than by a cookie the browser attaches automatically.
func TestWopiIsExemptFromOriginChecks(t *testing.T) {
	guard := reflect.ValueOf((&Server{}).sameOrigin).Pointer()

	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			if !strings.HasPrefix(route, "/wopi/") {
				return nil
			}
			for _, mw := range middlewares {
				if reflect.ValueOf(mw).Pointer() == guard {
					t.Errorf("%s %s would refuse the Document Server's own saves", method, route)
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
}

// The collab socket is a GET, so sameOrigin lets it through, yet it carries
// the session cookie into a socket that reads and writes a document. The
// route must be wrapped in socketOrigin — and that wrapper must refuse a
// foreign Origin and a missing one before anything behind it (ygo, which
// authorizes and loads the room before its own origin check) runs.
func TestCollabSocketRefusesForeignOrigins(t *testing.T) {
	guard := reflect.ValueOf((&Server{}).socketOrigin).Pointer()
	srv := &Server{externalURL: "https://hub.example"}

	var chain func(http.Handler) http.Handler
	err := chi.Walk(
		srv.Router().(chi.Routes),
		func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			if method != http.MethodGet || route != "/api/v1/collab/{room}" {
				return nil
			}
			for _, mw := range middlewares {
				if reflect.ValueOf(mw).Pointer() == guard {
					chain = mw
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if chain == nil {
		t.Fatal("GET /api/v1/collab/{room} is not wrapped in socketOrigin")
	}

	cases := []struct {
		name, origin string
		admitted     bool
	}{
		{"missing", "", false},
		{"foreign", "https://evil.example", false},
		{"same host, other scheme", "http://hub.example", false},
		{"this deployment", "https://hub.example", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			h := chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusSwitchingProtocols)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/collab/n.e", nil)
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			// A same-origin Referer must not stand in for a missing Origin.
			req.Header.Set("Referer", "https://hub.example/edit/n")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if reached != tc.admitted {
				t.Fatalf("upgrade reached: %v, want %v (status %d)", reached, tc.admitted, rec.Code)
			}
			if !tc.admitted && rec.Code != http.StatusForbidden {
				t.Fatalf("refusal status %d, want 403", rec.Code)
			}
		})
	}
}

// A link visitor has no session: the block-document routes of a link are
// registered with the other /s routes, outside the authenticated group, so
// the anonymous page can render the document and its images. The token and
// the node's place in the link authorize them instead (shareScope).
func TestShareDocumentRoutesArePublic(t *testing.T) {
	guard := reflect.ValueOf((&Server{}).authenticated).Pointer()
	want := map[string]bool{
		"GET /s/{token}/document": false, "GET /s/{token}/attachments/{hash}": false,
		"GET /s/{token}/page": false, "GET /s/{token}/pages": false,
	}
	err := chi.Walk(
		(&Server{}).Router().(chi.Routes),
		func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			key := method + " " + route
			if _, ok := want[key]; !ok {
				return nil
			}
			want[key] = true
			for _, mw := range middlewares {
				if reflect.ValueOf(mw).Pointer() == guard {
					t.Errorf("%s demands a session a link visitor does not have", key)
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	for route, seen := range want {
		if !seen {
			t.Errorf("%s is not registered", route)
		}
	}
}
