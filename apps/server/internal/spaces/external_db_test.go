package spaces_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// externalFixture is an organization space with a folder tree, its owner, and
// somebody outside it. The instance switch starts on and the space's own
// switch starts on, so each test turns off what it is about.
type externalFixture struct {
	q        *store.Queries
	sp       *spaces.Service
	sh       *share.Service
	instance *bool
	space    store.Space
	owner    store.User
	outsider store.User
	folder   store.Node
	inside   store.Node
	sibling  store.Node
}

func newExternalFixture(t *testing.T, label string) externalFixture {
	t.Helper()
	q, sp, sh := testEnv(t)
	ctx := context.Background()
	on := true
	sh.SetExternalSharing(func() bool { return on })
	f := externalFixture{q: q, sp: sp, sh: sh, instance: &on}
	f.owner = user(t, q, label+"-owner")
	f.outsider = user(t, q, label+"-outsider")
	space, err := sp.Create(ctx, fmt.Sprintf("ext-%s-%d", label, os.Getpid()), f.owner.ID)
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, space.ID) })
	f.space = setExternalSharing(t, q, space, true)
	mk := func(name string, parent pgtype.UUID, kind store.NodeKind, by store.User) store.Node {
		n, err := q.CreateNode(ctx, store.CreateNodeParams{
			SpaceID: space.ID, ParentID: parent, Kind: kind, Name: name, CreatedBy: by.ID,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return n
	}
	f.folder = mk("Shared out", pgtype.UUID{}, store.NodeKindDir, f.owner)
	f.inside = mk("inside.txt", f.folder.ID, store.NodeKindFile, f.owner)
	f.sibling = mk("Kept in", pgtype.UUID{}, store.NodeKindDir, f.owner)
	return f
}

func setExternalSharing(t *testing.T, q *store.Queries, sp store.Space, on bool) store.Space {
	t.Helper()
	updated, err := q.UpdateSpaceSettings(context.Background(), store.UpdateSpaceSettingsParams{
		ID: sp.ID, Name: sp.Name, Slug: sp.Slug, Description: sp.Description,
		DefaultAsAdmin: sp.DefaultAsAdmin, QuotaBytes: sp.QuotaBytes,
		TrashRetentionDays: sp.TrashRetentionDays, AllowPublicLinks: sp.AllowPublicLinks,
		ExternalSharing: on,
	})
	if err != nil {
		t.Fatalf("set external_sharing: %v", err)
	}
	return updated
}

func (f externalFixture) grant(t *testing.T, node store.Node, to store.User, caps share.GrantCaps, external bool) {
	t.Helper()
	if _, err := f.sh.UpsertDirect(context.Background(), node.ID, to.ID, caps, pgtype.Timestamptz{}, f.owner.ID, external); err != nil {
		t.Fatalf("grant: %v", err)
	}
}

// An external grant opens exactly what it names, down its subtree, at exactly
// its level — and nothing beside it.
func TestExternalGrantReachesItsSubtreeOnly(t *testing.T) {
	f := newExternalFixture(t, "subtree")
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelView, CanDownload: true}, true)

	for _, n := range []store.Node{f.folder, f.inside} {
		got := caps(t, f.sh, n, f.outsider.ID)
		if !got.View || !got.Download || got.Edit || got.Share || got.Owner {
			t.Errorf("%s: want view+download only, got %+v", n.Name, got)
		}
	}
	if got := caps(t, f.sh, f.sibling, f.outsider.ID); got.View {
		t.Errorf("a sibling of the shared folder must stay closed, got %+v", got)
	}
}

// A grant made for a member is not external, and does not open anything to
// that person once they are no longer one — only an is_external row reaches a
// non-member.
func TestOrdinaryGrantDoesNotReachANonMember(t *testing.T) {
	f := newExternalFixture(t, "ordinary")
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelEdit}, false)
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("a non-external direct grant must not reach a non-member, got %+v", got)
	}
}

// Ownership is a member's path. Somebody outside the space who added a file
// through an external edit grant reaches it through that grant — not as its
// owner — and loses it with the grant.
func TestNonMemberOwnsNothing(t *testing.T) {
	f := newExternalFixture(t, "owner")
	ctx := context.Background()
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelEdit}, true)
	theirs, err := f.q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID: f.space.ID, ParentID: f.folder.ID, Kind: store.NodeKindFile, Name: "theirs.txt", CreatedBy: f.outsider.ID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got := caps(t, f.sh, theirs, f.outsider.ID)
	if got.Owner || got.Share || !got.Edit {
		t.Errorf("want edit through the grant and no ownership, got %+v", got)
	}
	sh, ok, err := f.sh.DirectFor(ctx, f.folder.ID, f.outsider.ID)
	if err != nil || !ok {
		t.Fatalf("direct grant: %v %v", ok, err)
	}
	if _, err := f.sh.Revoke(ctx, f.folder.ID, sh.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := caps(t, f.sh, theirs, f.outsider.ID); got.View {
		t.Errorf("revoking the grant must end their reach into what they added, got %+v", got)
	}
}

// Group grants are a member's path too: a stale group row naming somebody
// outside the space opens nothing.
func TestGroupGrantNeverReachesANonMember(t *testing.T) {
	f := newExternalFixture(t, "group")
	ctx := context.Background()
	everyone, err := f.q.GetEveryoneGroup(ctx, f.space.ID)
	if err != nil {
		t.Fatalf("everyone: %v", err)
	}
	if err := f.q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: everyone.ID, UserID: f.outsider.ID}); err != nil {
		t.Fatalf("add group member: %v", err)
	}
	if _, err := f.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID: f.folder.ID, Level: store.ShareLevelEdit, CanDownload: true, GranteeGroupID: everyone.ID, CreatedBy: f.owner.ID,
	}); err != nil {
		t.Fatalf("group grant: %v", err)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("a group grant must not reach a non-member, got %+v", got)
	}
}

// Either switch off ends external access at once; on again restores it,
// because the rows were never touched.
func TestExternalAccessFollowsBothSwitches(t *testing.T) {
	f := newExternalFixture(t, "switches")
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelView}, true)

	*f.instance = false
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("instance switch off: want nothing, got %+v", got)
	}
	*f.instance = true
	f.space = setExternalSharing(t, f.q, f.space, false)
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("space switch off: want nothing, got %+v", got)
	}
	f.space = setExternalSharing(t, f.q, f.space, true)
	if got := caps(t, f.sh, f.inside, f.outsider.ID); !got.View {
		t.Errorf("both switches on again: want the grant back, got %+v", got)
	}
}

// A member holding an external-flagged grant — they joined after it was made —
// keeps every member path, switches or not.
func TestMemberKeepsAccessWhenSwitchesAreOff(t *testing.T) {
	f := newExternalFixture(t, "joined")
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelView}, true)
	if err := f.sp.AddMember(context.Background(), f.space.ID, f.outsider.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	*f.instance = false
	if got := caps(t, f.sh, f.inside, f.outsider.ID); !got.View {
		t.Errorf("a member's direct grant must not depend on the external switches, got %+v", got)
	}
}

// One personal space per person, however many times sign-in asks for it; it
// has no Archive and no organization, and its owner is its admin.
func TestEnsurePersonalIsIdempotent(t *testing.T) {
	q, sp, _ := testEnv(t)
	ctx := context.Background()
	owner := user(t, q, "personal-owner")
	first, created, err := sp.EnsurePersonal(ctx, owner, 1<<30)
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, first.ID) })
	again, created, err := sp.EnsurePersonal(ctx, owner, 1<<30)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second ensure must return the same space: created=%v err=%v", created, err)
	}
	if !spaces.IsPersonal(first) || first.IdpOrgID.Valid || first.ArchiveRootID.Valid || !first.ExternalSharing || first.QuotaBytes != 1<<30 {
		t.Errorf("unexpected personal space shape: %+v", first)
	}
	if !sp.IsAdmin(ctx, first.ID, owner.ID) {
		t.Error("the owner must be their personal space's admin")
	}
	if _, err := q.CreatePersonalSpace(ctx, store.CreatePersonalSpaceParams{Name: "x", OwnerID: owner.ID}); err == nil {
		t.Error("a second personal space for one owner must not be created")
	}
	// The check constraint keeps it unlinkable.
	if err := q.SetSpaceOrg(ctx, store.SetSpaceOrgParams{ID: first.ID, IdpOrgID: pgtype.Text{String: "org-x", Valid: true}}); err == nil {
		t.Error("a personal space must never be linked to an organization")
	}
}

// Invitations become grants for the verified address, once, external or not
// by the membership the account has at that moment; expired ones are dropped.
func TestAdoptPendingShares(t *testing.T) {
	f := newExternalFixture(t, "adopt")
	ctx := context.Background()
	email := share.NormalizeEmail("  Invitee-" + fmt.Sprint(os.Getpid()) + "@Example.test ")
	if _, inserted, err := f.sh.UpsertPending(ctx, f.folder.ID, email, share.GrantCaps{Level: share.LevelEdit}, pgtype.Timestamptz{}, f.owner.ID); err != nil || !inserted {
		t.Fatalf("invite: inserted=%v err=%v", inserted, err)
	}
	if _, inserted, err := f.sh.UpsertPending(ctx, f.folder.ID, email, share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, f.owner.ID); err != nil || inserted {
		t.Fatalf("re-invite must update, not insert: inserted=%v err=%v", inserted, err)
	}
	expired, _, err := f.sh.UpsertPending(ctx, f.sibling.ID, email, share.GrantCaps{Level: share.LevelView},
		pgtype.Timestamptz{Valid: true, Time: time.Now().Add(-time.Hour)}, f.owner.ID)
	if err != nil {
		t.Fatalf("expired invite: %v", err)
	}

	rows, err := f.sh.AdoptPending(ctx, f.outsider, email, false)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if len(rows) != 1 || rows[0].Share.NodeID != f.folder.ID || !rows[0].Share.IsExternal || rows[0].Share.Level != share.LevelView {
		t.Fatalf("want one external view grant on the folder, got %+v", rows)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); !got.View || got.Edit {
		t.Errorf("adopted grant: want view, got %+v", got)
	}
	if got := caps(t, f.sh, f.sibling, f.outsider.ID); got.View {
		t.Errorf("an expired invitation must not be adopted, got %+v", got)
	}
	if again, err := f.sh.AdoptPending(ctx, f.outsider, email, false); err != nil || len(again) != 0 {
		t.Errorf("adoption must happen once: %v %+v", err, again)
	}
	if _, err := f.sh.GetPending(ctx, f.sibling.ID, expired.ID); !errors.Is(err, share.ErrNotFound) {
		t.Errorf("expired invitations are deleted at adoption, got %v", err)
	}
}

// An invitation is the inviter's decision, deferred: once they have left the
// space it is dropped at adoption rather than honoured.
func TestAdoptionDropsADepartedInvitersInvitation(t *testing.T) {
	f := newExternalFixture(t, "adopt-left")
	ctx := context.Background()
	inviter := user(t, f.q, "adopt-left-inviter")
	if err := f.sp.AddMember(ctx, f.space.ID, inviter.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	f.grant(t, f.folder, inviter, share.GrantCaps{Level: share.LevelEdit, CanShare: true}, false)
	email := fmt.Sprintf("left-%d@example.test", os.Getpid())
	if _, _, err := f.sh.UpsertPending(ctx, f.folder.ID, email, share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, inviter.ID); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if err := f.sp.RevokeMembership(ctx, f.space.ID, inviter.ID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	rows, err := f.sh.AdoptPending(ctx, f.outsider, email, false)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a departed inviter's invitation must not be adopted: %v %+v", err, rows)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("want nothing, got %+v", got)
	}
}

// An external grant is a member's decision too: it stops resolving the moment
// its maker leaves the space, without waiting for an admin's handover, and
// comes back if they are re-added.
func TestExternalGrantEndsWithItsMakersMembership(t *testing.T) {
	f := newExternalFixture(t, "maker-left")
	ctx := context.Background()
	maker := user(t, f.q, "maker-left-maker")
	if err := f.sp.AddMember(ctx, f.space.ID, maker.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	f.grant(t, f.folder, maker, share.GrantCaps{Level: share.LevelEdit, CanShare: true}, false)
	if _, err := f.sh.UpsertDirect(ctx, f.folder.ID, f.outsider.ID, share.GrantCaps{Level: share.LevelView}, pgtype.Timestamptz{}, maker.ID, true); err != nil {
		t.Fatalf("external grant: %v", err)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); !got.View {
		t.Fatalf("precondition: the outsider reaches the folder, got %+v", got)
	}
	if err := f.sp.RevokeMembership(ctx, f.space.ID, maker.ID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("the maker left: want nothing, got %+v", got)
	}
}

// Joining turns someone's external grants into ordinary ones, so leaving again
// does not leave them on the outsider path.
func TestJoiningClearsTheExternalFlag(t *testing.T) {
	f := newExternalFixture(t, "rejoin")
	ctx := context.Background()
	f.grant(t, f.folder, f.outsider, share.GrantCaps{Level: share.LevelView}, true)
	if err := f.sp.AddMember(ctx, f.space.ID, f.outsider.ID, false); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := f.sp.RevokeMembership(ctx, f.space.ID, f.outsider.ID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if got := caps(t, f.sh, f.inside, f.outsider.ID); got.View {
		t.Errorf("after joining and leaving: want nothing, got %+v", got)
	}
}
