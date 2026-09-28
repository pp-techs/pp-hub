package orgsync_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func reloadSpace(t *testing.T, q *store.Queries, id pgtype.UUID) store.Space {
	t.Helper()
	sp, err := q.GetSpace(context.Background(), id)
	if err != nil {
		t.Fatalf("reload space: %v", err)
	}
	return sp
}

// Deletion does not travel. An organization deleted in Logto leaves its space
// in place, unlinked, with every member — not emptied by the next reconcile.
func TestDeletedOrganizationUnlinksTheSpaceAndKeepsItsMembers(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)
	before := memberIDs(t, q, space.ID)

	logto.missingOrgs[orgID] = true
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reloadSpace(t, q, space.ID).IdpOrgID.Valid {
		t.Error("space still linked to a deleted organization")
	}
	if got := memberIDs(t, q, space.ID); len(got) != len(before) {
		t.Errorf("members = %v, want %v kept", got, before)
	}
}

// The same fact can arrive first at a sign-in, as an organization missing
// from the token. It must unlink, not make the person a departure.
func TestSignInDoesNotEvictFromASpaceWhoseOrganizationWasDeleted(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)
	user := logtoAccount(t, q, logto, testIssuer, "member")
	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{Organizations: []string{orgID}}); err != nil {
		t.Fatalf("join: %v", err)
	}

	logto.missingOrgs[orgID] = true
	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{Organizations: []string{}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := sp.RequireMember(ctx, space.ID, user.ID); err != nil {
		t.Errorf("deleted organization evicted a member: %v", err)
	}
	if reloadSpace(t, q, space.ID).IdpOrgID.Valid {
		t.Error("space still linked to a deleted organization")
	}
}

// A space deleted here leaves its organization in Logto, and its members
// still carry it in their tokens. Their next sign-in must not bring an empty
// space back.
func TestSignInDoesNotRecreateADetachedOrganizationsSpace(t *testing.T) {
	q, _, logto, svc := testEnv(t)
	ctx := context.Background()
	user := logtoAccount(t, q, logto, testIssuer, "returning")
	orgID := "org-detached-" + user.OidcSub
	if err := q.DetachOrg(ctx, store.DetachOrgParams{OrgID: orgID}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	t.Cleanup(func() { _ = q.ReattachOrg(context.Background(), orgID) })

	if err := svc.ReconcileUser(ctx, user, orgsync.Claims{Organizations: []string{orgID}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := q.GetSpaceByOrgID(ctx, pgtype.Text{String: orgID, Valid: true}); err == nil {
		t.Error("sign-in recreated the space of a detached organization")
	}
}

// The name is the organization's: a rename in Logto reaches the space.
func TestReconcileMirrorsTheOrganizationName(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	space, orgID := linkedSpace(t, q, sp)

	logto.orgName = "Renamed Upstream"
	if err := svc.ReconcileOrg(ctx, orgID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := reloadSpace(t, q, space.ID).Name; got != "Renamed Upstream" {
		t.Errorf("space name = %q, want the organization's", got)
	}
}

// Creating a space creates the company upstream with its creator as admin,
// and the space that comes back is linked to it — not a local space nothing
// reconciles.
func TestCreateSpaceCreatesTheOrganizationWithTheCreatorAsAdmin(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	creator := logtoAccount(t, q, logto, testIssuer, "creator")

	space, err := svc.CreateSpace(ctx, creator, "New Co")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(context.Background(), space.ID) })
	if len(logto.created) != 1 || space.IdpOrgID.String != logto.created[0] {
		t.Fatalf("space linked to %q, organizations created %v", space.IdpOrgID.String, logto.created)
	}
	orgID := logto.created[0]
	if !slices.Contains(logto.added[orgID], creator.OidcSub) || !logto.admins[orgID+"/"+creator.OidcSub] {
		t.Error("the creator was not seated upstream as admin")
	}
	if !sp.IsAdmin(ctx, space.ID, creator.ID) {
		t.Error("the creator is not the space's admin")
	}
}

// Linking an existing space pushes its members up with their roles, links
// last, and turns the members Logto has no account for into departures —
// but only after the admin has been told about them.
func TestLinkPushesMembersAndDepartsTheOnesLogtoCannotHold(t *testing.T) {
	q, sp, logto, svc := testEnv(t)
	ctx := context.Background()
	owner := logtoAccount(t, q, logto, testIssuer, "owner")
	foreign := logtoAccount(t, q, logto, "https://other-idp.test", "foreign")
	space, err := sp.Create(ctx, "legacy-"+owner.OidcSub, owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(context.Background(), space.ID) })
	if err := sp.AddMember(ctx, space.ID, foreign.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if _, err := svc.Link(ctx, space, false); !errors.Is(err, orgsync.ErrStrandedMembers) {
		t.Fatalf("link without confirming stranded members = %v, want ErrStrandedMembers", err)
	}
	if len(logto.created) != 0 {
		t.Fatal("an organization was created before the admin confirmed")
	}
	orgID, err := svc.Link(ctx, space, true)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	for range 2 { // a retried job must land in the same state
		if err := svc.RunLink(ctx, space.ID, orgID); err != nil {
			t.Fatalf("run link: %v", err)
		}
	}

	if got := reloadSpace(t, q, space.ID).IdpOrgID.String; got != orgID {
		t.Fatalf("space linked to %q, want %q", got, orgID)
	}
	if !slices.Contains(logto.added[orgID], owner.OidcSub) || !logto.admins[orgID+"/"+owner.OidcSub] {
		t.Error("the owner was not pushed upstream as admin")
	}
	if err := sp.RequireMember(ctx, space.ID, foreign.ID); err == nil {
		t.Error("a member with no Logto account kept access after linking")
	}
	if _, pending := pendingFor(t, q, space.ID, foreign.Email); !pending {
		t.Error("the stranded member's content was not queued for handover")
	}
}
