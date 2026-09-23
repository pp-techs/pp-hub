// Package spaces implements space CRUD and membership.
//
// M15 model: membership is just membership — there is no role column. Admin
// authority is membership in the space's automatic Admin group, and it confers
// space-management powers only, never implicit file access. See
// docs/architecture/permissions.md.
package spaces

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound     = errors.New("space not found")
	ErrNotMember    = errors.New("not a member of this space")
	ErrForbidden    = errors.New("not an admin of this space")
	ErrMemberExists = errors.New("user is already a member")
	// ErrLastAdmin: removing or demoting them would leave the space
	// unadministrable, with no route back through the UI.
	ErrLastAdmin = errors.New("cannot remove the last admin")
)

// Service wraps space queries with membership checks.
type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// inTx runs fn with a copy of the service bound to one transaction. Nested
// calls — Create calling AddMember — open savepoints.
func (s *Service) inTx(ctx context.Context, fn func(*Service) error) error {
	return s.q.InTx(ctx, func(q *store.Queries) error {
		return fn(&Service{q: q})
	})
}

// EveryoneGroupName / AdminGroupName are the display names of the two
// automatic groups every space has. Both are immutable: no rename, no delete,
// no hand-edited membership (Everyone) — Admin membership is the admin role,
// managed through SetAdmin.
const (
	EveryoneGroupName = "Everyone"
	AdminGroupName    = "Admin"
	// ArchiveRootName is the folder that receives a departed member's content.
	ArchiveRootName = "Archive"
)

// Create makes a space with its two automatic groups and its archive root,
// and registers the creator as a member of both groups.
//
// The automatic groups are not optional: groups_everyone_uniq /
// groups_admin_uniq assume each exists, and migration 0015 created them for
// every pre-existing space. The archive root carries the one grant the server
// makes on its own: edit+download+share to the Admin group, so a departed
// member's content always has a custodian.
//
// All of it is one transaction: a space missing either group, its archive or
// the archive grant breaks invariants the rest of the system assumes.
func (s *Service) Create(ctx context.Context, name string, ownerID pgtype.UUID) (store.Space, error) {
	var sp store.Space
	err := s.inTx(ctx, func(s *Service) error {
		var err error
		sp, err = s.create(ctx, name, ownerID)
		return err
	})
	return sp, err
}

func (s *Service) create(ctx context.Context, name string, ownerID pgtype.UUID) (store.Space, error) {
	sp, err := s.q.CreateSpace(ctx, store.CreateSpaceParams{Name: name, OwnerID: ownerID})
	if err != nil {
		return store.Space{}, err
	}
	// Everyone must exist before AddMember looks it up.
	if _, err := s.q.CreateGroup(ctx, store.CreateGroupParams{
		SpaceID:     sp.ID,
		Name:        EveryoneGroupName,
		Description: "Everyone in this space. Membership is maintained automatically.",
		IsEveryone:  true,
		CreatedBy:   ownerID,
	}); err != nil {
		return store.Space{}, err
	}
	admin, err := s.q.CreateGroup(ctx, store.CreateGroupParams{
		SpaceID:     sp.ID,
		Name:        AdminGroupName,
		Description: "Space administrators. Membership is the admin role; managed by admins.",
		IsAdmin:     true,
		CreatedBy:   ownerID,
	})
	if err != nil {
		return store.Space{}, err
	}
	archive, err := s.q.CreateNode(ctx, store.CreateNodeParams{
		SpaceID:   sp.ID,
		Kind:      store.NodeKindDir,
		Name:      ArchiveRootName,
		CreatedBy: ownerID,
	})
	if err != nil {
		return store.Space{}, err
	}
	if err := s.q.SetArchiveRoot(ctx, store.SetArchiveRootParams{
		ID: sp.ID, ArchiveRootID: archive.ID,
	}); err != nil {
		return store.Space{}, err
	}
	sp.ArchiveRootID = archive.ID
	if _, err := s.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID:         archive.ID,
		Level:          store.ShareLevelEdit,
		CanDownload:    true,
		CanShare:       true,
		GranteeGroupID: admin.ID,
		CreatedBy:      ownerID,
	}); err != nil {
		return store.Space{}, err
	}
	if err := s.AddMember(ctx, sp.ID, ownerID, true); err != nil {
		return store.Space{}, err
	}
	return sp, nil
}

func (s *Service) ListForUser(ctx context.Context, userID pgtype.UUID) ([]store.Space, error) {
	return s.q.ListMySpaces(ctx, userID)
}

func (s *Service) Get(ctx context.Context, id pgtype.UUID) (store.Space, error) {
	sp, err := s.q.GetSpace(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Space{}, ErrNotFound
	}
	return sp, err
}

// RequireMember errors unless the user belongs to the space.
func (s *Service) RequireMember(ctx context.Context, spaceID, userID pgtype.UUID) error {
	ok, err := s.q.IsSpaceMember(ctx, store.IsSpaceMemberParams{SpaceID: spaceID, UserID: userID})
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	return nil
}

// IsAdmin reports whether the user is in the space's Admin group.
func (s *Service) IsAdmin(ctx context.Context, spaceID, userID pgtype.UUID) bool {
	ok, err := s.q.IsSpaceAdmin(ctx, store.IsSpaceAdminParams{SpaceID: spaceID, UserID: userID})
	return err == nil && ok
}

// RequireAdmin errors unless the user is in the space's Admin group.
func (s *Service) RequireAdmin(ctx context.Context, spaceID, userID pgtype.UUID) error {
	ok, err := s.q.IsSpaceAdmin(ctx, store.IsSpaceAdminParams{SpaceID: spaceID, UserID: userID})
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

// AddMember adds the user to the space and its Everyone group, and to the
// Admin group when asAdmin. Only admins may manage members (checked by
// caller). One transaction, so "member of the space" and "member of
// Everyone" never disagree.
func (s *Service) AddMember(ctx context.Context, spaceID, userID pgtype.UUID, asAdmin bool) error {
	return s.inTx(ctx, func(s *Service) error { return s.addMember(ctx, spaceID, userID, asAdmin) })
}

func (s *Service) addMember(ctx context.Context, spaceID, userID pgtype.UUID, asAdmin bool) error {
	if err := s.q.AddSpaceMember(ctx, store.AddSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userID,
	}); err != nil {
		return err
	}
	everyone, err := s.q.GetEveryoneGroup(ctx, spaceID)
	if err != nil {
		return err
	}
	if err := s.q.AddGroupMember(ctx, store.AddGroupMemberParams{
		GroupID: everyone.ID,
		UserID:  userID,
	}); err != nil {
		return err
	}
	if asAdmin {
		admin, err := s.q.GetAdminGroup(ctx, spaceID)
		if err != nil {
			return err
		}
		if err := s.q.AddGroupMember(ctx, store.AddGroupMemberParams{
			GroupID: admin.ID,
			UserID:  userID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SetAdmin adds or removes the user in the space's Admin group — the only
// thing "admin" means anymore. Removing the last admin is refused: the space
// would be unadministrable with no route back through the UI.
//
// Promotion requires the target to already be a member. Without that check
// an admin could seat a non-member in the Admin group, producing somebody who
// passes IsSpaceAdmin — and therefore every space-administration route, since
// those check admin status and not membership — while having no
// space_members row, so the People page does not list them. An administrator
// nobody can see is worse than one nobody wanted. The equivalent check
// already guards user-created groups, and the Admin group is the higher
// privilege of the two.
func (s *Service) SetAdmin(ctx context.Context, spaceID, userID pgtype.UUID, asAdmin bool) error {
	if !asAdmin {
		if err := s.RequireNotLastAdmin(ctx, spaceID, userID); err != nil {
			return err
		}
	}
	return s.ApplyAdmin(ctx, spaceID, userID, asAdmin)
}

// ApplyAdmin is SetAdmin without the last-admin refusal: the sync path
// reporting what the organization already says, rather than a request asking
// for a change that may be declined.
func (s *Service) ApplyAdmin(ctx context.Context, spaceID, userID pgtype.UUID, asAdmin bool) error {
	admin, err := s.q.GetAdminGroup(ctx, spaceID)
	if err != nil {
		return err
	}
	if asAdmin {
		if err := s.RequireMember(ctx, spaceID, userID); err != nil {
			return err
		}
		return s.q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: admin.ID, UserID: userID})
	}
	return s.q.RemoveGroupMember(ctx, store.RemoveGroupMemberParams{GroupID: admin.ID, UserID: userID})
}

// RequireNotLastAdmin refuses an operation that would leave the space with no
// administrator.
//
// It guards the *request* paths only — an admin demoting or removing somebody
// through pp-hub. The sync path does not consult it, and must not: a removal
// performed in the identity provider has already happened, and a mirror that
// declines to mirror it is simply wrong. A space that ends up with no admin
// says so; see spaceDTO.AdminCount.
func (s *Service) RequireNotLastAdmin(ctx context.Context, spaceID, userID pgtype.UUID) error {
	if !s.IsAdmin(ctx, spaceID, userID) {
		return nil
	}
	count, err := s.q.CountSpaceAdmins(ctx, spaceID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// RevokeMembership takes away everything membership grants, and nothing else.
//
// This is the half of a departure that must happen the instant it is known:
// the space_members row, so every listing and guard stops matching, and the
// group memberships, because group_members references users rather than
// spaces and therefore cascades from neither — leaving it would keep a
// departed colleague's group grants live.
//
// Nothing here is destructive: no grant is dropped, no content moves. The
// caller holds no access once the membership row is gone, so the grants left
// standing grant nothing, and what remains can wait for CompleteDeparture and
// a human. Idempotent, because every sync path may observe the same departure.
func (s *Service) RevokeMembership(ctx context.Context, spaceID, userID pgtype.UUID) error {
	return s.inTx(ctx, func(s *Service) error {
		if err := s.q.RemoveUserFromSpaceGroups(ctx, store.RemoveUserFromSpaceGroupsParams{
			SpaceID: spaceID,
			UserID:  userID,
		}); err != nil {
			return err
		}
		return s.q.RemoveSpaceMember(ctx, store.RemoveSpaceMemberParams{SpaceID: spaceID, UserID: userID})
	})
}

// CompleteDeparture hands over what a departed member left behind.
//
// Destructive and irreversible, which is why it is a separate, admin-confirmed
// step rather than something a webhook can trigger. Order matters:
//
//  1. Grants die first — inbound (direct grants naming them) and outbound
//     (everything they created, links included) — while the nodes still have
//     their original owner, so the departed member's sharing decisions leave
//     with them.
//  2. Ownership of nodes sitting under somebody else's folder climbs to the
//     nearest ancestor's creator: the content stays where the work is.
//  3. What remains — top-level subtrees they wholly owned — moves under the
//     space's archive root, which the Admin group holds a grant on. Nothing is
//     deleted; admins keep a backup they can read, restore or purge.
//
// The membership rows are already gone by the time this runs: RevokeMembership
// dropped them when the departure was detected.
//
// The whole hand-over is one transaction. Half of it — grants gone but roots
// not archived, or ownership climbed but the archive grant not re-seated —
// is a state no reader of the tree expects, and retrying from it would not
// be idempotent.
func (s *Service) CompleteDeparture(ctx context.Context, spaceID, userID, actorID pgtype.UUID) error {
	return s.inTx(ctx, func(s *Service) error { return s.completeDeparture(ctx, spaceID, userID, actorID) })
}

func (s *Service) completeDeparture(ctx context.Context, spaceID, userID, actorID pgtype.UUID) error {
	if err := s.q.DeleteSharesByGranteeInSpace(ctx, store.DeleteSharesByGranteeInSpaceParams{
		SpaceID: spaceID, GranteeUserID: userID,
	}); err != nil {
		return err
	}
	if err := s.q.DeleteSharesByCreatorInSpace(ctx, store.DeleteSharesByCreatorInSpaceParams{
		SpaceID: spaceID, CreatedBy: userID,
	}); err != nil {
		return err
	}
	if err := s.q.TransferNestedOwnership(ctx, store.TransferNestedOwnershipParams{
		SpaceID: spaceID, UserID: userID,
	}); err != nil {
		return err
	}
	sp, err := s.Get(ctx, spaceID)
	if err != nil {
		return err
	}
	if sp.ArchiveRootID.Valid {
		roots, err := s.q.ListArchiveRoots(ctx, store.ListArchiveRootsParams{
			SpaceID: spaceID, CreatedBy: userID,
		})
		if err != nil {
			return err
		}
		for _, rootID := range roots {
			if err := s.moveToArchive(ctx, rootID, sp.ArchiveRootID); err != nil {
				return err
			}
		}
		// The departed member may have been the grant's creator (space
		// creation and migration both use the owner), and step 1 deleted
		// their grants. Re-seat it so the archive never loses its custodian.
		if admin, err := s.q.GetAdminGroup(ctx, spaceID); err == nil {
			if _, err := s.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
				NodeID:         sp.ArchiveRootID,
				Level:          store.ShareLevelEdit,
				CanDownload:    true,
				CanShare:       true,
				GranteeGroupID: admin.ID,
				CreatedBy:      actorID,
			}); err != nil {
				return err
			}
		}
	}
	// A team folder belongs to the company, so it does not follow its creator
	// into the archive — but step 1 deleted the grants they made, and one of
	// those may have been the only manager grant on it. A team folder nobody
	// can administer is a dead end: no member can be added, and no person owns
	// the content. Hand it to the Admin group, the same custodian the archive
	// gets, so an admin can appoint the next manager.
	if admin, err := s.q.GetAdminGroup(ctx, spaceID); err == nil {
		teamRoots, err := s.q.ListTeamRoots(ctx, spaceID)
		if err != nil {
			return err
		}
		for _, rootID := range teamRoots {
			managers, err := s.q.CountTeamManagers(ctx, rootID)
			if err != nil {
				return err
			}
			if managers > 0 {
				continue
			}
			if _, err := s.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
				NodeID:         rootID,
				Level:          store.ShareLevelEdit,
				CanDownload:    true,
				CanShare:       true,
				GranteeGroupID: admin.ID,
				CreatedBy:      actorID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// moveToArchive re-parents one root under the archive folder, renaming with a
// " (n)" suffix when a live sibling already holds the name — the same
// collision rule fs.Restore uses.
func (s *Service) moveToArchive(ctx context.Context, nodeID, archiveRoot pgtype.UUID) error {
	// Every write that may collide runs in its own savepoint: inside the
	// departure transaction a unique violation would otherwise abort the lot.
	try := func(fn func(q *store.Queries) error) error { return s.q.InTx(ctx, fn) }
	err := try(func(q *store.Queries) error {
		return q.MoveUnderArchiveRoot(ctx, store.MoveUnderArchiveRootParams{ID: nodeID, ParentID: archiveRoot})
	})
	if !isUniqueViolation(err) {
		return err
	}
	node, err := s.q.GetNodeAny(ctx, nodeID)
	if err != nil {
		return err
	}
	for attempt := 2; attempt <= 50; attempt++ {
		name := fmt.Sprintf("%s (%d)", node.Name, attempt)
		if err := try(func(q *store.Queries) error {
			return q.RenameNode(ctx, store.RenameNodeParams{ID: nodeID, Name: name})
		}); err == nil {
			return s.q.MoveUnderArchiveRoot(ctx, store.MoveUnderArchiveRootParams{
				ID: nodeID, ParentID: archiveRoot,
			})
		} else if !isUniqueViolation(err) {
			return err
		}
	}
	return errors.New("no free name for archived node")
}

// Members lists a space's members with the per-member figures the settings
// table shows — files owned here, last sign-in, and whether they are in the
// Admin group.
func (s *Service) Members(ctx context.Context, spaceID pgtype.UUID) ([]store.ListSpaceMembersDetailedRow, error) {
	return s.q.ListSpaceMembersDetailed(ctx, spaceID)
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
