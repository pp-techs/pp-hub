package httpapi

import (
	"net/http"
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
	"GET /api/v1/users",
	"GET /api/v1/admin/stats",
	"PATCH /api/v1/admin/users/{userID}",
	"POST /api/v1/invitations/accept",
	"GET /api/v1/users/search",
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
	"GET /api/v1/spaces/{spaceID}/invitations",
	"POST /api/v1/spaces/{spaceID}/invitations",
	"DELETE /api/v1/spaces/{spaceID}/invitations/{inviteID}",

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
	"POST /api/v1/spaces/{spaceID}/files",
	"GET /api/v1/spaces/{spaceID}/members/",
	"POST /api/v1/spaces/{spaceID}/members/",
	"PATCH /api/v1/spaces/{spaceID}/members/{userID}/",
	"DELETE /api/v1/spaces/{spaceID}/members/{userID}/",

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
	"POST /api/v1/nodes/{nodeID}/edit-session",

	"GET /s/{token}/",
	"GET /s/{token}/download",
	"GET /s/{token}/children",
	"GET /s/{token}/content",
	"GET /s/{token}/node-download",
	"GET /s/{token}/download-all",
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
