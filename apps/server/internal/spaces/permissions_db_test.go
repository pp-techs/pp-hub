package spaces_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
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
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
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
	if err := sp.RequireNotLastAdmin(ctx, space.ID, admin.ID); !errors.Is(err, spaces.ErrLastAdmin) {
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

	depart(t, ctx, sp, space.ID, leaver.ID, admin.ID)

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

// Removing the person who created the space must leave the space usable.
//
// They own the Archive folder — spaces.Service creates it in their name — so
// ListArchiveRoots handed it back as one of "their roots" and
// MoveUnderArchiveRoot parented it to itself. NodeAccessCaps walks ancestors
// with UNION ALL, so from then on resolving anything inside the Archive never
// returned: every node-addressed request in the space hung onto its
// connection until the client gave up. Hence the deadline here.
func TestRemovingTheSpaceCreatorLeavesTheArchiveWalkable(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	creator := user(t, q, "founder")
	successor := user(t, q, "successor")

	space, err := sp.Create(ctx, fmt.Sprintf("founder-%d", os.Getpid()), creator.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, successor.ID, true); err != nil {
		t.Fatalf("add admin: %v", err)
	}

	depart(t, ctx, sp, space.ID, creator.ID, successor.ID)

	archive, err := q.GetNodeAny(ctx, space.ArchiveRootID)
	if err != nil {
		t.Fatalf("reload archive: %v", err)
	}
	if archive.ParentID.Valid {
		t.Fatalf("the archive root must stay at the top level, got parent %v", archive.ParentID)
	}

	// Anything filed in the Archive must still resolve.
	filed, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, ParentID: archive.ID, Kind: store.NodeKindFile,
		Name: "handover.txt", CreatedBy: successor.ID,
	})
	if err != nil {
		t.Fatalf("create in archive: %v", err)
	}
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := q.NodeAccessCaps(deadline, store.NodeAccessCapsParams{
		NodeIds: []pgtype.UUID{filed.ID}, UserID: successor.ID,
	}); err != nil {
		t.Fatalf("resolving caps inside the archive: %v", err)
	}
}

// A node under another node the caller reaches is not an entry point of its
// own — you get to it by walking down from the ancestor.
//
// This is a regression test with teeth: the ancestor walk used to re-emit the
// same parent on every recursive step under UNION ALL, so it never terminated
// once any reachable node had a parent at all. The symptom was not a wrong
// answer, it was the space's root listing and folder tree hanging until the
// browser gave up — which is why the call here carries its own deadline
// instead of relying on the suite's.
func TestNestedOwnedNodesAreNotSeparateEntryPoints(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	owner := user(t, q, "nested-owner")
	space, err := sp.Create(ctx, fmt.Sprintf("nested-%d", os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })

	top, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Top", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create top: %v", err)
	}
	mid, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Mid",
		ParentID: top.ID, CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create mid: %v", err)
	}
	if _, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindFile, Name: "deep.txt",
		ParentID: mid.ID, CreatedBy: owner.ID,
	}); err != nil {
		t.Fatalf("create deep file: %v", err)
	}

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	entries, err := q.ListEntryPoints(deadline, store.ListEntryPointsParams{
		UserID: owner.ID, IncludeOwned: true, SpaceID: space.ID,
	})
	if err != nil {
		t.Fatalf("entry points over a nested tree: %v", err)
	}
	// The space's own Archive folder is a legitimate second entry point — the
	// creator owns it — so this asserts the containment rule rather than a
	// count: the top folder is listed, what sits under it is not.
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Name] = true
	}
	if !listed["Top"] {
		t.Errorf("the top folder is missing from the entry points, got %v", keys(listed))
	}
	for _, under := range []string{"Mid", "deep.txt"} {
		if listed[under] {
			t.Errorf("%q sits under Top and must not be its own entry point, got %v", under, keys(listed))
		}
	}
}

// A colleague's file must not disappear because the folder around it was
// trashed, and a purge must not destroy it.
//
// Trashing tombstones one row; the subtree stays live but unreachable by
// walking down. The entry-point query used to drop those nodes anyway — the
// trashed folder still counted as a reachable ancestor — so the uploader's own
// file vanished from their home with no trace, and the folder owner's purge
// then took it with the cascade. Both halves are checked here.
func TestTrashedFolderNeitherHidesNorDestroysAColleaguesFile(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	host := user(t, q, "trash-host")
	guest := user(t, q, "trash-guest")

	space, err := sp.Create(ctx, fmt.Sprintf("trash-%d", os.Getpid()), host.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, guest.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}

	folder, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Folder1", CreatedBy: host.ID,
	})
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)
	guestFile, _, err := files.PutFile(ctx, space.ID, folder.ID, pgtype.UUID{},
		"guest.txt", strings.NewReader("guest content"), guest.ID)
	if err != nil {
		t.Fatalf("guest upload: %v", err)
	}

	if err := q.SoftDeleteNode(ctx, store.SoftDeleteNodeParams{ID: folder.ID, DeletedBy: host.ID}); err != nil {
		t.Fatalf("trash the folder: %v", err)
	}

	entries, err := q.ListEntryPoints(ctx, store.ListEntryPointsParams{
		UserID: guest.ID, IncludeOwned: true, SpaceID: space.ID,
	})
	if err != nil {
		t.Fatalf("entry points: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.ID == guestFile.ID {
			found = true
		}
	}
	if !found {
		t.Error("the guest's file vanished from their own top level when the folder around it was trashed")
	}

	// And the trash says where it went, even though the guest holds nothing on
	// the folder itself.
	buried, err := files.TrashRootsWithUserContent(ctx, space.ID, guest.ID)
	if err != nil {
		t.Fatalf("buried content: %v", err)
	}
	if buried[folder.ID] != 1 {
		t.Errorf("the guest's trash should report 1 item buried under Folder1, got %d", buried[folder.ID])
	}

	rescued, err := files.Purge(ctx, folder.ID)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if len(rescued) != 1 {
		t.Fatalf("purge should have rescued the guest's file, rescued %d subtrees", len(rescued))
	}
	survivor, err := q.GetNode(ctx, guestFile.ID)
	if err != nil {
		t.Fatalf("the purge destroyed a file its owner never trashed: %v", err)
	}
	if survivor.ParentID.Valid {
		t.Errorf("the rescued file should sit at its owner's top level, got parent %v", survivor.ParentID)
	}
	if survivor.CreatedBy != guest.ID {
		t.Error("the rescue must not change who owns the file")
	}
}

// Uploading a name somebody already used creates a new file for the uploader.
//
// It used to commit a new version of the existing node instead, which handed
// the uploader nothing — the node kept its original owner — while silently
// overwriting work. Ownership must not depend on whether a filename happened
// to collide.
func TestUploadingAnExistingNameCreatesANewFileForTheUploader(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	host := user(t, q, "collide-host")
	guest := user(t, q, "collide-guest")

	space, err := sp.Create(ctx, fmt.Sprintf("collide-%d", os.Getpid()), host.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, guest.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	folder, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Shared", CreatedBy: host.ID,
	})
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)

	original, _, err := files.PutFile(ctx, space.ID, folder.ID, pgtype.UUID{},
		"spec.txt", strings.NewReader("host version"), host.ID)
	if err != nil {
		t.Fatalf("host upload: %v", err)
	}
	second, _, err := files.PutFile(ctx, space.ID, folder.ID, pgtype.UUID{},
		"spec.txt", strings.NewReader("guest version"), guest.ID)
	if err != nil {
		t.Fatalf("guest upload: %v", err)
	}

	if second.ID == original.ID {
		t.Fatal("the guest's upload overwrote the host's file instead of creating their own")
	}
	if second.CreatedBy != guest.ID {
		t.Error("the uploader must own what they uploaded")
	}
	if second.Name != "spec (2).txt" {
		t.Errorf("colliding upload should land as \"spec (2).txt\", got %q", second.Name)
	}
	if versions, err := q.ListVersions(ctx, original.ID); err != nil {
		t.Fatalf("versions: %v", err)
	} else if len(versions) != 1 {
		t.Errorf("the host's file should still have exactly one version, got %d", len(versions))
	}
}

// Moving a node changes who reaches it, and the move dialog has to say so.
//
// This is the one edge case deliberately left permitted: a member may move a
// file they created out of a colleague's folder, which is how they take it
// back. What must not happen is doing it — or the reverse, dropping a file
// into a shared folder — without being told who it affects.
func TestMoveImpactNamesWhoGainsAndLosesAccess(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	host := user(t, q, "impact-host")
	guest := user(t, q, "impact-guest")

	space, err := sp.Create(ctx, fmt.Sprintf("impact-%d", os.Getpid()), host.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, guest.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)

	shared, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Shared", CreatedBy: host.ID,
	})
	if err != nil {
		t.Fatalf("shared folder: %v", err)
	}
	if _, err := q.CreateShare(ctx, store.CreateShareParams{
		NodeID: shared.ID, Kind: store.ShareKindDirect, GranteeUserID: guest.ID,
		Level: store.ShareLevelEdit, CanDownload: true, CreatedBy: host.ID,
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	private, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Private", CreatedBy: guest.ID,
	})
	if err != nil {
		t.Fatalf("private folder: %v", err)
	}
	file, _, err := files.PutFile(ctx, space.ID, shared.ID, pgtype.UUID{},
		"draft.txt", strings.NewReader("draft"), guest.ID)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Guest pulls their file out of the host's folder: the host loses it.
	out, err := files.MoveImpact(ctx, file, private.ID)
	if err != nil {
		t.Fatalf("move impact out: %v", err)
	}
	if !mentions(out.Losing, host.ID) {
		t.Errorf("moving out of the host's folder must report the host losing access, got %+v", out.Losing)
	}
	if mentions(out.Losing, guest.ID) || mentions(out.Gaining, guest.ID) {
		t.Errorf("the file's own owner neither gains nor loses, got losing %+v gaining %+v", out.Losing, out.Gaining)
	}

	// And the reverse: a file at the guest's top level moving into the shared
	// folder hands it to the host.
	loose, _, err := files.PutFile(ctx, space.ID, pgtype.UUID{}, pgtype.UUID{},
		"loose.txt", strings.NewReader("loose"), guest.ID)
	if err != nil {
		t.Fatalf("upload loose: %v", err)
	}
	in, err := files.MoveImpact(ctx, loose, shared.ID)
	if err != nil {
		t.Fatalf("move impact in: %v", err)
	}
	if !mentions(in.Gaining, host.ID) {
		t.Errorf("moving into the host's folder must report the host gaining access, got %+v", in.Gaining)
	}
	if len(in.Losing) != 0 {
		t.Errorf("moving out of the top level takes nothing away, got %+v", in.Losing)
	}
}

func mentions(ps []fs.Principal, id pgtype.UUID) bool {
	for _, p := range ps {
		if p.Kind == "user" && p.ID == id {
			return true
		}
	}
	return false
}

// A team folder owns its content: `created_by` there is a byline, not access.
//
// This is the whole point of the container. In a personal folder the uploader
// keeps full capabilities on their file forever — revoking the folder grant
// does not reach it, they can always share it onward, and they can move it
// out. Inside a team folder none of that is true, and this test pins each of
// those three differences.
func TestTeamFolderContentHasNoPersonalOwner(t *testing.T) {
	q, sp, sh := testEnv(t)
	ctx := context.Background()

	manager := user(t, q, "team-manager")
	member := user(t, q, "team-member")

	space, err := sp.Create(ctx, fmt.Sprintf("team-%d", os.Getpid()), manager.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, member.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)

	team, err := files.CreateTeamFolder(ctx, space.ID, "Marketing", manager.ID)
	if err != nil {
		t.Fatalf("team folder: %v", err)
	}
	if team.TeamRootID != team.ID {
		t.Fatalf("a team folder must be its own container, got %v", team.TeamRootID)
	}
	// Its creator reaches it through the manager grant creation seeds, not
	// through ownership.
	if got := caps(t, sh, team, manager.ID); got.Owner || !got.Share || !got.Edit {
		t.Errorf("the creator must be a manager, not an owner, got %+v", got)
	}
	contributor, err := q.CreateShare(ctx, store.CreateShareParams{
		NodeID: team.ID, Kind: store.ShareKindDirect, GranteeUserID: member.ID,
		Level: store.ShareLevelEdit, CanDownload: true, CanShare: false, CreatedBy: manager.ID,
	})
	if err != nil {
		t.Fatalf("contributor grant: %v", err)
	}

	file, _, err := files.PutFile(ctx, space.ID, team.ID, pgtype.UUID{},
		"campaign.txt", strings.NewReader("copy"), member.ID)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if file.TeamRootID != team.ID {
		t.Errorf("content created inside a team folder belongs to it, got %v", file.TeamRootID)
	}

	// Difference 1: the uploader is a contributor, not an owner, so the
	// "no re-sharing" half of their grant actually holds.
	got := caps(t, sh, file, member.ID)
	if !got.Edit || got.Owner || got.Share {
		t.Errorf("the uploader should edit but neither own nor re-share, got %+v", got)
	}
	// The manager's can_share reaches down the container as usual.
	if got := caps(t, sh, file, manager.ID); !got.Share {
		t.Errorf("a manager must be able to share team content, got %+v", got)
	}

	// Difference 2: revoking the grant actually removes access. The node is
	// part of the predicate now, so the share's own node has to be named.
	if ok, err := sh.Revoke(ctx, team.ID, contributor.ID); err != nil || !ok {
		t.Fatalf("revoke: ok=%v err=%v", ok, err)
	}
	if got := caps(t, sh, file, member.ID); got.View || got.Edit || got.Owner {
		t.Errorf("revoking a team grant must remove access to what the grantee uploaded, got %+v", got)
	}
	// Difference 3: and it does not resurface at their own top level.
	entries, err := q.ListEntryPoints(ctx, store.ListEntryPointsParams{
		UserID: member.ID, IncludeOwned: true, SpaceID: space.ID,
	})
	if err != nil {
		t.Fatalf("entry points: %v", err)
	}
	for _, e := range entries {
		if e.ID == file.ID {
			t.Error("team content must never become somebody's personal entry point")
		}
	}
}

// Moving content out of a team folder is an export: it leaves the container
// and needs a personal owner, which is the mover. Drive transfers ownership
// the same way when an item leaves a shared drive.
func TestMovingOutOfATeamFolderReassignsTheAuthor(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	manager := user(t, q, "export-manager")
	member := user(t, q, "export-member")

	space, err := sp.Create(ctx, fmt.Sprintf("export-%d", os.Getpid()), manager.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, member.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)

	team, err := files.CreateTeamFolder(ctx, space.ID, "Ops", manager.ID)
	if err != nil {
		t.Fatalf("team folder: %v", err)
	}
	inner, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, ParentID: team.ID, Kind: store.NodeKindDir,
		Name: "Runbooks", CreatedBy: member.ID,
	})
	if err != nil {
		t.Fatalf("inner folder: %v", err)
	}
	leaf, _, err := files.PutFile(ctx, space.ID, inner.ID, pgtype.UUID{},
		"deploy.txt", strings.NewReader("steps"), member.ID)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// A team folder itself stays where it is.
	if err := files.Move(ctx, team.ID, inner.ID, manager.ID); !errors.Is(err, fs.ErrTeamRootFixed) {
		t.Errorf("a team folder must refuse to be nested, got %v", err)
	}

	// The manager exports the subfolder to their own top level.
	if err := files.Move(ctx, inner.ID, pgtype.UUID{}, manager.ID); err != nil {
		t.Fatalf("move out: %v", err)
	}
	movedDir, err := q.GetNode(ctx, inner.ID)
	if err != nil {
		t.Fatalf("reload dir: %v", err)
	}
	movedLeaf, err := q.GetNode(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("reload leaf: %v", err)
	}
	if movedDir.TeamRootID.Valid || movedLeaf.TeamRootID.Valid {
		t.Error("the whole subtree must leave the container, not just the node that moved")
	}
	if movedDir.CreatedBy != manager.ID || movedLeaf.CreatedBy != manager.ID {
		t.Error("content leaving a team folder becomes the mover's, so it has an owner at all")
	}
	// Authorship survives where it belongs: on the versions.
	versions, err := q.ListVersions(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) != 1 || versions[0].CreatedBy != member.ID {
		t.Error("the version history must still name whoever actually wrote the file")
	}
}

// A team folder belongs to the company, so it must not follow its creator into
// the archive when they leave — and it must not be left with no manager.
func TestTeamFolderSurvivesItsCreatorsDeparture(t *testing.T) {
	q, sp, sh := testEnv(t)
	ctx := context.Background()

	founder := user(t, q, "leaving-founder")
	successor := user(t, q, "staying-admin")

	space, err := sp.Create(ctx, fmt.Sprintf("teamdep-%d", os.Getpid()), founder.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, successor.ID, true); err != nil {
		t.Fatalf("add admin: %v", err)
	}
	blobs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	files := fs.NewService(q, blobs)
	team, err := files.CreateTeamFolder(ctx, space.ID, "Finance", founder.ID)
	if err != nil {
		t.Fatalf("team folder: %v", err)
	}

	depart(t, ctx, sp, space.ID, founder.ID, successor.ID)

	after, err := q.GetNode(ctx, team.ID)
	if err != nil {
		t.Fatalf("reload team folder: %v", err)
	}
	if after.ParentID.Valid {
		t.Error("a team folder must stay at the top level when its creator leaves, not go to the archive")
	}
	if got := caps(t, sh, after, successor.ID); !got.Share {
		t.Errorf("a team folder left without managers must fall to the Admin group, got %+v", got)
	}
}

// Losing membership ends reach at once, including the listing: between
// RevokeMembership and an admin confirming the departure, the direct grants
// naming the leaver still exist, and "Shared with me" used to keep listing
// what they named — names, sizes, owners — although nothing resolved.
func TestDepartedMemberNoLongerListsWhatWasSharedWithThem(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()

	owner := user(t, q, "listing-owner")
	leaver := user(t, q, "listing-leaver")
	space, err := sp.Create(ctx, fmt.Sprintf("listing-%d", os.Getpid()), owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	if err := sp.AddMember(ctx, space.ID, leaver.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	plan, err := q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: space.ID, Kind: store.NodeKindDir, Name: "Plans", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := q.CreateShare(ctx, store.CreateShareParams{
		NodeID: plan.ID, Kind: store.ShareKindDirect, GranteeUserID: leaver.ID,
		Level: store.ShareLevelView, CreatedBy: owner.ID,
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	listed := func() bool {
		rows, err := q.ListEntryPoints(ctx, store.ListEntryPointsParams{UserID: leaver.ID})
		if err != nil {
			t.Fatalf("entry points: %v", err)
		}
		for _, r := range rows {
			if r.ID == plan.ID {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("a member must see what was shared with them")
	}
	if err := sp.RevokeMembership(ctx, space.ID, leaver.ID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if listed() {
		t.Error("a departed member must not list what was shared with them, pending confirmation or not")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// depart runs both halves of a departure: the revocation every sync path
// performs immediately, then the handover an admin confirms. Together they
// are what RemoveMember used to do in one call, and the assertions below are
// about the state they leave behind.
func depart(t *testing.T, ctx context.Context, sp *spaces.Service, spaceID, userID, actorID pgtype.UUID) {
	t.Helper()
	if err := sp.RevokeMembership(ctx, spaceID, userID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if err := sp.CompleteDeparture(ctx, spaceID, userID, actorID); err != nil {
		t.Fatalf("complete departure: %v", err)
	}
}
