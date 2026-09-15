// Package spaces implements space CRUD and membership.
// See docs/architecture/permissions.md for the role model.
package spaces

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound     = errors.New("space not found")
	ErrNotMember    = errors.New("not a member of this space")
	ErrForbidden    = errors.New("insufficient space role")
	ErrMemberExists = errors.New("user is already a member")
)

// Role is the space-scoped permission level.
//
// There are only two, and a member's role grants no access to any file: it says
// they belong to the company, nothing more. What a member can reach comes
// entirely from group grants and direct shares on individual folders — see
// docs/architecture/permissions.md. An admin administers the space and holds
// edit everywhere in it.
type Role = store.MemberRole

const (
	RoleAdmin  = store.MemberRoleAdmin
	RoleMember = store.MemberRoleMember
)

// Service wraps space queries with membership checks.
type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// EveryoneGroupName is the display name of the automatic group every space has.
const EveryoneGroupName = "Everyone"

// Create makes a space, registers the creator as its admin, and creates the
// automatic "Everyone" group.
//
// The Everyone group is not optional: it is how a member who has been granted
// nothing individually gets any access at all, and groups_everyone_uniq assumes
// it exists. Migration 0011 created one for every pre-existing space; this
// keeps new spaces at parity.
func (s *Service) Create(ctx context.Context, name string, ownerID pgtype.UUID) (store.Space, error) {
	sp, err := s.q.CreateSpace(ctx, store.CreateSpaceParams{Name: name, OwnerID: ownerID})
	if err != nil {
		return store.Space{}, err
	}
	if err := s.q.AddSpaceMember(ctx, store.AddSpaceMemberParams{
		SpaceID: sp.ID,
		UserID:  ownerID,
		Role:    RoleAdmin,
	}); err != nil {
		return store.Space{}, err
	}
	if _, err := s.q.CreateGroup(ctx, store.CreateGroupParams{
		SpaceID:     sp.ID,
		Name:        EveryoneGroupName,
		Description: "Everyone in this space. Membership is maintained automatically.",
		IsEveryone:  true,
		CreatedBy:   ownerID,
	}); err != nil {
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

// RoleOf returns the user's role in a space.
func (s *Service) RoleOf(ctx context.Context, spaceID, userID pgtype.UUID) (Role, error) {
	role, err := s.q.GetMemberRole(ctx, store.GetMemberRoleParams{SpaceID: spaceID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotMember
	}
	return role, err
}

// RequireRole errors unless the user holds at least minRole.
// Order: member < admin.
//
// This is a role-vs-role comparison and is NOT the permission scale. Node
// access runs on view=1/download=2/edit=3 (see share.EffectivePerm); the two
// scales are deliberately separate.
func (s *Service) RequireRole(ctx context.Context, spaceID, userID pgtype.UUID, min Role) error {
	role, err := s.RoleOf(ctx, spaceID, userID)
	if err != nil {
		return err
	}
	if roleRank(role) < roleRank(min) {
		return ErrForbidden
	}
	return nil
}

func roleRank(r Role) int {
	switch r {
	case RoleAdmin:
		return 2
	case RoleMember:
		return 1
	default:
		return 0
	}
}

// AddMember grants a role and puts the user in the space's Everyone group.
// Only admins may manage members (checked by caller).
func (s *Service) AddMember(ctx context.Context, spaceID, userID pgtype.UUID, role Role) error {
	if err := s.q.AddSpaceMember(ctx, store.AddSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userID,
		Role:    role,
	}); err != nil {
		return err
	}
	everyone, err := s.q.GetEveryoneGroup(ctx, spaceID)
	if err != nil {
		return err
	}
	return s.q.AddGroupMember(ctx, store.AddGroupMemberParams{
		GroupID: everyone.ID,
		UserID:  userID,
	})
}

// RemoveMember drops the membership row and every group membership in that
// space.
//
// The group cleanup is the point: group_members references users, not spaces,
// so nothing cascades when the space_members row goes. Skipping it leaves
// someone who is no longer a member of the company still holding live access
// to every folder their old groups were granted.
func (s *Service) RemoveMember(ctx context.Context, spaceID, userID pgtype.UUID) error {
	if err := s.q.RemoveUserFromSpaceGroups(ctx, store.RemoveUserFromSpaceGroupsParams{
		SpaceID: spaceID,
		UserID:  userID,
	}); err != nil {
		return err
	}
	return s.q.RemoveSpaceMember(ctx, store.RemoveSpaceMemberParams{SpaceID: spaceID, UserID: userID})
}

// Members lists a space's members with the per-member figures the settings
// table shows — files owned here, and last sign-in.
func (s *Service) Members(ctx context.Context, spaceID pgtype.UUID) ([]store.ListSpaceMembersDetailedRow, error) {
	return s.q.ListSpaceMembersDetailed(ctx, spaceID)
}
