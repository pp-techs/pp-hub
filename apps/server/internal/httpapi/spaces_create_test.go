package httpapi

import (
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// orgServer builds a Server whose orgsync carries the given settings. Only
// the configuration is exercised here — canCreateSpaces makes no call.
func orgServer(cfg config.LogtoSettings) *Server {
	return &Server{orgs: orgsync.NewService(nil, nil, nil, orgsync.NewClient(cfg), "iss")}
}

var (
	noTenant   = config.LogtoSettings{}
	claimOnly  = config.LogtoSettings{Endpoint: "https://tenant.logto.app", AdminRole: "admin"}
	fullTenant = config.LogtoSettings{
		Endpoint: "https://tenant.logto.app", ClientID: "m2m", ClientSecret: "secret", AdminRole: "admin",
	}
)

// A space is a company. Once organizations decide who belongs to which, an
// ordinary member minting one has awarded themselves the admin role the
// organization was supposed to decide — and produced a space no reconcile
// will ever touch. The instance admin keeps the escape hatch.
func TestSpaceCreationIsClosedToMembersWhenOrganizationsOwnMembership(t *testing.T) {
	s := orgServer(fullTenant)
	if s.canCreateSpaces(&store.User{}) {
		t.Error("an ordinary member may still create a space")
	}
	if !s.canCreateSpaces(&store.User{IsAdmin: true}) {
		t.Error("the instance admin lost the escape hatch for an unlinked space")
	}
}

// The gate keys on organizations being authoritative, not on the Management
// API being reachable. An install running from sign-in claims alone still has
// the identity provider deciding which companies exist — and it is the mode
// an operator is in before creating a machine-to-machine application, which
// is exactly when a member clicking "new space" would do the damage.
func TestSpaceCreationIsClosedInClaimOnlyMode(t *testing.T) {
	s := orgServer(claimOnly)
	if s.canCreateSpaces(&store.User{}) {
		t.Error("a member may create a space while organizations own membership")
	}
	if !s.canCreateSpaces(&store.User{IsAdmin: true}) {
		t.Error("the instance admin lost the escape hatch")
	}
}

// With no Logto tenant configured, pp-hub is still the only place spaces can
// come from, so closing the route would leave an install with no way to make
// its first one.
func TestSpaceCreationStaysOpenWithoutATenant(t *testing.T) {
	if !orgServer(noTenant).canCreateSpaces(&store.User{}) {
		t.Error("space creation was closed on an install with no organization sync")
	}
}
