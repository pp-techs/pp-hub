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
type Role = store.MemberRole

const (
	RoleOwner  = store.MemberRoleOwner
	RoleEditor = store.MemberRoleEditor
	RoleViewer = store.MemberRoleViewer
)

// Service wraps space queries with membership checks.
type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// Create makes a space and registers the creator as owner member.
func (s *Service) Create(ctx context.Context, name string, ownerID pgtype.UUID) (store.Space, error) {
	sp, err := s.q.CreateSpace(ctx, store.CreateSpaceParams{Name: name, OwnerID: ownerID})
	if err != nil {
		return store.Space{}, err
	}
	if err := s.q.AddSpaceMember(ctx, store.AddSpaceMemberParams{
		SpaceID: sp.ID,
		UserID:  ownerID,
		Role:    RoleOwner,
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
// Order: viewer < editor < owner.
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
	case RoleOwner:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// AddMember grants a role. Only owners may manage members (checked by caller).
func (s *Service) AddMember(ctx context.Context, spaceID, userID pgtype.UUID, role Role) error {
	return s.q.AddSpaceMember(ctx, store.AddSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userID,
		Role:    role,
	})
}

func (s *Service) RemoveMember(ctx context.Context, spaceID, userID pgtype.UUID) error {
	return s.q.RemoveSpaceMember(ctx, store.RemoveSpaceMemberParams{SpaceID: spaceID, UserID: userID})
}

func (s *Service) Members(ctx context.Context, spaceID pgtype.UUID) ([]store.ListSpaceMembersRow, error) {
	return s.q.ListSpaceMembers(ctx, spaceID)
}
