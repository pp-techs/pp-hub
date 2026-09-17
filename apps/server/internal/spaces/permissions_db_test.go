package spaces_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The M15 model lives in SQL — ownership inherits through a recursive CTE and
// admin status is a row in group_members — so the invariants that matter
// cannot be asserted against a fake. Same contract as
// internal/preview/jobs_db_test.go: a real Postgres or a loud skip.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/spaces/
func testEnv(t *testing.T) (*store.Queries, *spaces.Service, *share.Service) {
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
	return q, spaces.NewService(q), share.NewService(q)
}

// user creates a throwaway account. oidc_sub is unique per call so runs do not
// collide with each other or with real dev data.
func user(t *testing.T, q *store.Queries, label string) store.User {
	t.Helper()
	sub := fmt.Sprintf("test-%s-%d", label, os.Getpid())
	u, err := q.UpsertUser(context.Background(), store.UpsertUserParams{
		OidcSub: sub,
		OidcIss: "test",
		Email:   sub + "@test.local",
		Name:    label,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	return u
}

func caps(t *testing.T, sh *share.Service, node store.Node, userID pgtype.UUID) share.Caps {
	t.Helper()
	c, err := sh.EffectiveCapsForNode(context.Background(), node, userID)
	if err != nil {
		t.Fatalf("resolve caps: %v", err)
	}
	return c
}

// A member's own folder is private from the space's admins. This is the whole
// point of M15: before it, an admin held an edit floor over the entire tree.
func TestAdminCannotReadAMembersOwnFolder(t *testing.T) {
	q, sp, sh := testEnv(t)
	ctx := context.Background()

	admin := user(t, q, "admin")
	member := user(t, q, "member")
	bystander := user(t, q, "bystander")

	space, err := sp.Create(ctx, fmt.Sprintf("caps-%d", os.Getpid()), admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	for _, u := range []store.User{member, bystander} {
		if err := sp.AddMember(ctx, space.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}

	dir, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Private", CreatedBy: member.ID,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	if got := caps(t, sh, dir, member.ID); !got.Owner || !got.Edit || !got.Share {
		t.Errorf("creator should hold full capabilities on their own folder, got %+v", got)
	}
	if got := caps(t, sh, dir, admin.ID); got.View || got.Download || got.Edit || got.Share {
		t.Errorf("an admin must resolve to nothing on a member's folder, got %+v", got)
	}
	if got := caps(t, sh, dir, bystander.ID); got.View {
		t.Errorf("another member must resolve to nothing, got %+v", got)
	}

	// A grant to Everyone opens exactly what it says — and no more.
	everyone, err := q.GetEveryoneGroup(ctx, space.ID)
	if err != nil {
		t.Fatalf("everyone group: %v", err)
	}
	if _, err := q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID: dir.ID, Level: store.ShareLevelView, GranteeGroupID: everyone.ID, CreatedBy: member.ID,
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	got := caps(t, sh, dir, bystander.ID)
	if !got.View || got.Download || got.Edit || got.Share {
		t.Errorf("a view grant must open view only, got %+v", got)
	}
}

// Ownership inherits down the subtree: whoever owns the folder reaches what
// colleagues put inside it, and the uploader still owns their own node.
func TestOwnershipInheritsDownTheSubtree(t *testing.T) {
	q, sp, sh := testEnv(t)
	ctx := context.Background()

	owner := user(t, q, "owner")
	guest := user(t, q, "guest")

	space, err := sp.Create(ctx, fmt.Sprintf("inherit-%d", os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, guest.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}

	dir, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Shared work", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create dir: %v", err)
	}
	inner, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, ParentID: dir.ID, Kind: store.NodeKindFile, Name: "guest.txt", CreatedBy: guest.ID,
	})
	if err != nil {
		t.Fatalf("create inner: %v", err)
	}

	if got := caps(t, sh, inner, owner.ID); !got.Edit {
		t.Errorf("the folder owner must reach nodes created inside it, got %+v", got)
	}
	if got := caps(t, sh, inner, guest.ID); !got.Owner {
		t.Errorf("the uploader owns their own node, got %+v", got)
	}
}

// Admin status is Admin-group membership, so promoting and demoting must both
// work — demotion once silently did nothing because it went through AddMember.
func TestSetAdminPromotesDemotesAndKeepsALastAdmin(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	admin := user(t, q, "first-admin")
	second := user(t, q, "second-admin")

	space, err := sp.Create(ctx, fmt.Sprintf("admins-%d", os.Getpid()), admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, second.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := sp.SetAdmin(ctx, space.ID, second.ID, true); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !sp.IsAdmin(ctx, space.ID, second.ID) {
		t.Fatal("promote did not take effect")
	}
	if err := sp.SetAdmin(ctx, space.ID, second.ID, false); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if sp.IsAdmin(ctx, space.ID, second.ID) {
		t.Fatal("demote did not take effect — admin membership survived")
	}
	if err := sp.SetAdmin(ctx, space.ID, admin.ID, false); !errors.Is(err, spaces.ErrLastAdmin) {
		t.Errorf("demoting the last admin = %v, want ErrLastAdmin", err)
	}
	if err := sp.RemoveMember(ctx, space.ID, admin.ID, admin.ID); !errors.Is(err, spaces.ErrLastAdmin) {
		t.Errorf("removing the last admin = %v, want ErrLastAdmin", err)
	}
}

// Leaving hands the work to the admins without handing over readership of
// anything still live: the root lands in Archive, outgoing grants die.
func TestRemoveMemberArchivesTheirRootsAndRevokesGrants(t *testing.T) {
	q, sp, sh := testEnv(t)
	ctx := context.Background()

	admin := user(t, q, "keeper")
	leaver := user(t, q, "leaver")
	other := user(t, q, "stayer")

	space, err := sp.Create(ctx, fmt.Sprintf("archive-%d", os.Getpid()), admin.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	for _, u := range []store.User{leaver, other} {
		if err := sp.AddMember(ctx, space.ID, u.ID, false); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}

	dir, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Leaver work", CreatedBy: leaver.ID,
	})
	if err != nil {
		t.Fatalf("create dir: %v", err)
	}
	everyone, err := q.GetEveryoneGroup(ctx, space.ID)
	if err != nil {
		t.Fatalf("everyone: %v", err)
	}
	if _, err := q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID: dir.ID, Level: store.ShareLevelView, GranteeGroupID: everyone.ID, CreatedBy: leaver.ID,
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if err := sp.RemoveMember(ctx, space.ID, leaver.ID, admin.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	moved, err := q.GetNodeAny(ctx, dir.ID)
	if err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if moved.ParentID != space.ArchiveRootID {
		t.Error("the departed member's root was not moved under the archive root")
	}
	rows, err := q.ListSharesForNode(ctx, dir.ID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("the departed member's grants survived: %d rows", len(rows))
	}
	if got := caps(t, sh, moved, admin.ID); !got.View {
		t.Errorf("an admin must be able to read the archive, got %+v", got)
	}
	if got := caps(t, sh, moved, other.ID); got.View {
		t.Errorf("a plain member must not read the archive, got %+v", got)
	}
	// The point of archiving is custody. If the person who left still resolved
	// to owner on what they made, removal would have moved the folder and
	// changed nothing about who can read it.
	if got := caps(t, sh, moved, leaver.ID); got.View || got.Edit || got.Owner {
		t.Errorf("the departed member still reaches their archived work, got %+v", got)
	}
	entries, err := q.ListEntryPoints(ctx, store.ListEntryPointsParams{
		UserID: leaver.ID, IncludeOwned: true, SpaceID: space.ID,
	})
	if err != nil {
		t.Fatalf("entry points: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the departed member still lists %d entry points in the space", len(entries))
	}
}
