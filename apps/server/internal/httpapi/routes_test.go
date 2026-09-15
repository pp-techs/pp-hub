package httpapi

import (
	"net/http"
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
	"GET /api/v1/me",
	"GET /api/v1/users/me",
	"PUT /api/v1/users/me",
	"POST /api/v1/users/me/avatar",
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
