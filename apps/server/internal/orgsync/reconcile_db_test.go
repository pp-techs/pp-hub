package orgsync_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Reconciliation is a diff between an HTTP response and several tables, and
// the tables are the half that can go wrong invisibly — a revoked member who
// keeps a group row still holds every grant that group carries. So this runs
// against a real Postgres and a real HTTP server, and skips loudly without
// one:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/orgsync/
const testIssuer = "https://logto.test/oidc"

// fakeLogto serves the two Management API calls reconcile makes, plus the
// token endpoint. Its roster is swapped between reconciles to model somebody
// joining or leaving upstream.
type fakeLogto struct {
	t       *testing.T
	roster  []map[string]any
	calls   int
	server  *httptest.Server
	orgName string
}

func newFakeLogto(t *testing.T) *fakeLogto {
	t.Helper()
	f := &fakeLogto{t: t, orgName: "Acme"}
	mux := http.NewServeMux()
	mux.HandleFunc("/oidc/token", func(w http.ResponseWriter, r *http.Request) {
		if id, secret, ok := r.BasicAuth(); !ok || id != "m2m" || secret != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"access_token": "token", "expires_in": 3600})
	})
	mux.HandleFunc("/api/organizations/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/users") {
			f.calls++
			writeJSON(w, f.roster)
			return
		}
		writeJSON(w, map[string]any{"id": "org_acme", "name": f.orgName})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeLogto) settings() config.LogtoSettings {
	return config.LogtoSettings{
		Endpoint:     f.server.URL,
		ClientID:     "m2m",
		ClientSecret: "secret",
		APIResource:  "https://default.logto.app/api",
		AdminRole:    "admin",
	}
}

func member(sub, email string, admin bool) map[string]any {
	m := map[string]any{"id": sub, "primaryEmail": email, "name": email}
	if admin {
		m["organizationRoles"] = []map[string]any{{"id": "r1", "name": "admin"}}
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func testEnv(t *testing.T) (*store.Queries, *spaces.Service, *fakeLogto, *orgsync.Service) {
	t.Helper()
	url := os.Getenv("PP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PP_TEST_DATABASE_URL not set; skipping database-backed test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := store.New(pool)
	sp := spaces.NewService(q)
	logto := newFakeLogto(t)
	return q, sp, logto, orgsync.NewService(q, sp, orgsync.NewClient(logto.settings()), testIssuer)
}

// linkedSpace creates a space owned by a throwaway account and points it at
// an organization id unique to this run.
func linkedSpace(t *testing.T, q *store.Queries, sp *spaces.Service) (store.Space, string) {
	t.Helper()
	ctx := context.Background()
	orgID := fmt.Sprintf("org-%d-%s", os.Getpid(), t.Name())
	owner, err := q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: "owner-" + orgID, OidcIss: testIssuer,
		Email: "owner-" + orgID + "@test.local", Name: "owner",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	space, err := sp.Create(ctx, "space-"+orgID, owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := q.SetSpaceOrg(ctx, store.SetSpaceOrgParams{
		ID: space.ID, IdpOrgID: pgtype.Text{String: orgID, Valid: true},
	}); err != nil {
		t.Fatalf("link space: %v", err)
	}
	space.IdpOrgID = pgtype.Text{String: orgID, Valid: true}
	return space, orgID
}

func memberIDs(t *testing.T, q *store.Queries, spaceID pgtype.UUID) map[string]bool {
	t.Helper()
	rows, err := q.ListSpaceMembersDetailed(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Email] = r.IsAdmin
	}
	return out
}

func pendingFor(t *testing.T, q *store.Queries, spaceID pgtype.UUID, email string) (store.ListPendingDeparturesRow, bool) {
	t.Helper()
	rows, err := q.ListPendingDepartures(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("list departures: %v", err)
	}
	for _, row := range rows {
		if row.Email == email {
			return row, true
		}
	}
	return store.ListPendingDeparturesRow{}, false
}

// The sign-in path is the one that works with no Management API at all: an
// endpoint is named, the ID token carries the organization claims, and
// nothing calls out. It is also the only thing standing between a linked
// space and a stale membership when webhooks are not configured, so its two
// directions — joining and leaving — are worth pinning.
func TestSignInClaimsJoinTheLinkedSpaceWithTheOrganizationRole(t *testing.T) {
	q, sp, _, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)

	user, err := q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: "sub-arrival-" + orgID, OidcIss: testIssuer,
		Email: "arrival-" + orgID + "@test.local", Name: "arrival",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{
		Organizations:     []string{orgID},
		OrganizationRoles: []string{orgID + ":admin"},
	}); err != nil {
		t.Fatalf("reconcile from claims: %v", err)
	}
	if err := sp.RequireMember(ctx, space.ID, user.ID); err != nil {
		t.Fatalf("claims did not make them a member: %v", err)
	}
	if !sp.IsAdmin(ctx, space.ID, user.ID) {
		t.Error("the organization's admin role did not reach the Admin group")
	}

	// Demotion upstream must travel too: signing in again with the same
	// membership and no admin role takes the group away.
	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{Organizations: []string{orgID}}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if sp.IsAdmin(ctx, space.ID, user.ID) {
		t.Error("losing the organization role left them in the Admin group")
	}
}

// A token that no longer lists an organization is a departure nobody told us
// about. It must revoke immediately and queue the handover, exactly as the
// webhook path does — otherwise an install without webhooks never notices
// anybody leaving.
func TestSignInClaimsRevokeASpaceTheTokenNoLongerLists(t *testing.T) {
	q, sp, _, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)
	email := "gone-" + orgID + "@test.local"

	user, err := q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: "sub-gone-" + orgID, OidcIss: testIssuer, Email: email, Name: "gone",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{Organizations: []string{orgID}}); err != nil {
		t.Fatalf("join: %v", err)
	}

	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{}); err != nil {
		t.Fatalf("reconcile with no organizations: %v", err)
	}
	if err := sp.RequireMember(ctx, space.ID, user.ID); err == nil {
		t.Error("they are still a member of a space their token no longer lists")
	}
	row, ok := pendingFor(t, q, space.ID, email)
	if !ok {
		t.Fatal("no handover was queued for the departure")
	}
	if row.Source != orgsync.SourceSignin {
		t.Errorf("source = %q, want %q", row.Source, orgsync.SourceSignin)
	}
}

// An unlinked space is nobody's projection. A token that says nothing about
// it must not evict its members — which is what makes "link it when you are
// ready" a safe migration rather than a cliff.
func TestSignInClaimsLeaveUnlinkedSpacesAlone(t *testing.T) {
	q, sp, _, svc := testEnv(t)
	ctx := context.Background()

	// Unique per run, like linkedSpace: these rows outlive the test.
	tag := fmt.Sprintf("local-%d-%s", os.Getpid(), t.Name())
	owner, err := q.CreateUser(ctx, store.CreateUserParams{
		OidcSub: "sub-" + tag, OidcIss: testIssuer,
		Email: tag + "@test.local", Name: "local",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	space, err := sp.Create(ctx, "unlinked-"+tag, owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	if err := svc.ReconcileUser(ctx, owner, orgsync.Claims{Organizations: []string{"org-elsewhere"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := sp.RequireMember(ctx, space.ID, owner.ID); err != nil {
		t.Errorf("an unlinked space lost its member to somebody else's claims: %v", err)
	}
}

// The organization is the source of truth, including for people who have
// never signed in here. Refusing to pre-create their accounts would make the
// People page show a subset of the company, which is the disagreement this
// whole mechanism exists to remove.
func TestReconcilePreProvisionsMembersAndTheirAdminRole(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)

	logto.roster = []map[string]any{
		member("sub-boss-"+orgID, "boss-"+orgID+"@test.local", true),
		member("sub-hand-"+orgID, "hand-"+orgID+"@test.local", false),
	}
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := memberIDs(t, q, space.ID)
	if admin, ok := got["boss-"+orgID+"@test.local"]; !ok || !admin {
		t.Errorf("boss: member=%v admin=%v, want both true", ok, admin)
	}
	if admin, ok := got["hand-"+orgID+"@test.local"]; !ok || admin {
		t.Errorf("hand: member=%v admin=%v, want member and not admin", ok, admin)
	}
	// The space's own owner was never in the organization, so the mirror
	// must have removed them — this is the direction that actually enforces
	// the boundary.
	if len(got) != 2 {
		t.Errorf("membership = %v, want exactly the two organization members", got)
	}
}

// Running it twice must not change anything. Every delivery path calls this,
// and webhook deliveries are neither ordered nor deduplicated.
func TestReconcileIsIdempotent(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)

	logto.roster = []map[string]any{member("sub-one-"+orgID, "one-"+orgID+"@test.local", true)}
	for i := range 3 {
		if err := svc.ReconcileOrg(ctx, orgID); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if got := memberIDs(t, q, space.ID); len(got) != 1 {
		t.Errorf("membership after three reconciles = %v, want one row", got)
	}
}

// Leaving the organization must take away access immediately and take away
// nothing else. Group membership is the part that cannot wait: group_members
// references users rather than spaces, so nothing cascades when the
// membership row goes, and a departed colleague would keep every grant their
// groups carry.
func TestDepartureRevokesAccessNowAndQueuesTheHandover(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)
	email := "leaver-" + orgID + "@test.local"

	logto.roster = []map[string]any{
		member("sub-keeper-"+orgID, "keeper-"+orgID+"@test.local", true),
		member("sub-leaver-"+orgID, email, false),
	}
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	leaver, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("leaver not provisioned: %v", err)
	}
	everyone, err := q.GetEveryoneGroup(ctx, space.ID)
	if err != nil {
		t.Fatalf("everyone group: %v", err)
	}

	logto.roster = logto.roster[:1]
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if err := sp.RequireMember(ctx, space.ID, leaver.ID); err == nil {
		t.Error("the departed member is still a member of the space")
	}
	still, err := q.IsGroupMember(ctx, store.IsGroupMemberParams{
		GroupID: everyone.ID, UserID: leaver.ID,
	})
	if err != nil {
		t.Fatalf("group membership: %v", err)
	}
	if still {
		t.Error("departed member still sits in the Everyone group — its grants are live for them")
	}
	// The space's own creator was never in the organization, so the first
	// reconcile queued them too. This assertion is about the leaver's row.
	row, ok := pendingFor(t, q, space.ID, email)
	if !ok {
		t.Fatal("no pending departure for the leaver")
	}
	if row.Source != orgsync.SourceReconcile {
		t.Errorf("source = %q, want %q", row.Source, orgsync.SourceReconcile)
	}

	// Rejoining cancels the handover. Nothing was moved, so there is nothing
	// to undo — only a queue entry that stopped being true.
	logto.roster = append(logto.roster, member("sub-leaver-"+orgID, email, false))
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if _, ok := pendingFor(t, q, space.ID, email); ok {
		t.Error("rejoining left the handover queued")
	}
	if err := sp.RequireMember(ctx, space.ID, leaver.ID); err != nil {
		t.Errorf("rejoined member is not a member: %v", err)
	}
}

// An organization nobody has linked is not a reason to create a space —
// spaces.owner_id is NOT NULL and a reconcile has no user to name. It must be
// a silent no-op, not an error that fails the job forever.
func TestReconcileSkipsAnUnlinkedOrganization(t *testing.T) {
	_, _, _, svc := testEnv(t)
	if err := svc.ReconcileOrg(context.Background(), "org-nobody-mirrors"); err != nil {
		t.Errorf("reconcile of an unmirrored organization = %v, want nil", err)
	}
}
