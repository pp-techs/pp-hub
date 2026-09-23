// Package groups implements named sets of users inside a space and the grants
// that give those sets access to folders.
//
// See docs/architecture/permissions.md. Every space has two automatic,
// immutable groups: Everyone (membership mirrors space membership) and Admin
// (membership IS the admin role). Everything else is user-created.
package groups

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound = errors.New("group not found")
	// ErrSystemGroupImmutable guards the automatic groups. Everyone's
	// membership mirrors space membership and Admin's IS the admin role; both
	// are maintained by spaces.Service, never edited by hand.
	ErrSystemGroupImmutable = errors.New("this group is maintained automatically")
	ErrNameTaken            = errors.New("a group with that name already exists in this space")
	ErrNameEmpty            = errors.New("group name is required")
	// ErrCrossSpace is the isolation boundary: a grant may only name a group
	// from the same space as the node.
	ErrCrossSpace = errors.New("group and node belong to different spaces")
)

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service { return &Service{q: q} }

func (s *Service) List(ctx context.Context, spaceID pgtype.UUID) ([]store.ListGroupsRow, error) {
	return s.q.ListGroups(ctx, spaceID)
}

func (s *Service) Get(ctx context.Context, id pgtype.UUID) (store.Group, error) {
	g, err := s.q.GetGroup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Group{}, ErrNotFound
	}
	return g, err
}

func (s *Service) Create(ctx context.Context, spaceID pgtype.UUID, name, description string, createdBy pgtype.UUID) (store.Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Group{}, ErrNameEmpty
	}
	g, err := s.q.CreateGroup(ctx, store.CreateGroupParams{
		SpaceID:     spaceID,
		Name:        name,
		Description: strings.TrimSpace(description),
		IsEveryone:  false,
		IsAdmin:     false,
		CreatedBy:   createdBy,
	})
	if isUniqueViolation(err) {
		return store.Group{}, ErrNameTaken
	}
	return g, err
}

func (s *Service) Update(ctx context.Context, id pgtype.UUID, name, description string) (store.Group, error) {
	g, err := s.Get(ctx, id)
	if err != nil {
		return store.Group{}, err
	}
	if g.IsEveryone || g.IsAdmin {
		return store.Group{}, ErrSystemGroupImmutable
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Group{}, ErrNameEmpty
	}
	updated, err := s.q.UpdateGroup(ctx, store.UpdateGroupParams{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(description),
	})
	if isUniqueViolation(err) {
		return store.Group{}, ErrNameTaken
	}
	return updated, err
}

// Delete removes a group and, by cascade, every grant it held. Callers should
// surface GrantCount first: people lose access silently otherwise.
func (s *Service) Delete(ctx context.Context, id pgtype.UUID) error {
	g, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if g.IsEveryone || g.IsAdmin {
		return ErrSystemGroupImmutable
	}
	return s.q.DeleteGroup(ctx, id)
}

// GrantCount is how many folders a group opens — what a delete would take away.
func (s *Service) GrantCount(ctx context.Context, id pgtype.UUID) (int32, error) {
	return s.q.CountGrantsForGroup(ctx, id)
}

func (s *Service) Members(ctx context.Context, id pgtype.UUID) ([]store.ListGroupMembersRow, error) {
	return s.q.ListGroupMembers(ctx, id)
}

// AddMember puts a user in a group. The caller must have checked that the user
// is a member of the group's space — a group is scoped to one company and must
// never carry someone from another.
func (s *Service) AddMember(ctx context.Context, groupID, userID pgtype.UUID) error {
	g, err := s.Get(ctx, groupID)
	if err != nil {
		return err
	}
	// Everyone's membership mirrors space membership; Admin's is the admin
	// role. Both change only through spaces.Service.
	if g.IsEveryone || g.IsAdmin {
		return ErrSystemGroupImmutable
	}
	return s.q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: groupID, UserID: userID})
}

func (s *Service) RemoveMember(ctx context.Context, groupID, userID pgtype.UUID) error {
	g, err := s.Get(ctx, groupID)
	if err != nil {
		return err
	}
	if g.IsEveryone || g.IsAdmin {
		return ErrSystemGroupImmutable
	}
	return s.q.RemoveGroupMember(ctx, store.RemoveGroupMemberParams{GroupID: groupID, UserID: userID})
}

// --- Grants ---

// Grant gives a group capabilities on a node. node and group must share a
// space; otherwise a grant would punch a hole straight through the company
// boundary.
func (s *Service) Grant(ctx context.Context, node store.Node, groupID pgtype.UUID, caps share.GrantCaps, createdBy pgtype.UUID) (store.Share, error) {
	g, err := s.Get(ctx, groupID)
	if err != nil {
		return store.Share{}, err
	}
	if g.SpaceID != node.SpaceID {
		return store.Share{}, ErrCrossSpace
	}
	caps = caps.Normalized()
	return s.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID:         node.ID,
		Level:          caps.Level,
		CanDownload:    caps.CanDownload,
		CanShare:       caps.CanShare,
		GranteeGroupID: groupID,
		CreatedBy:      createdBy,
	})
}

// Revoke removes a group's grant on a node, reporting whether there was one:
// "revoked" and "there was nothing to revoke" are different answers.
func (s *Service) Revoke(ctx context.Context, nodeID, groupID pgtype.UUID) (bool, error) {
	n, err := s.q.DeleteGroupGrant(ctx, store.DeleteGroupGrantParams{
		NodeID:         nodeID,
		GranteeGroupID: groupID,
	})
	return n > 0, err
}

func (s *Service) GrantsForNode(ctx context.Context, nodeID pgtype.UUID) ([]store.ListGrantsForNodeRow, error) {
	return s.q.ListGrantsForNode(ctx, nodeID)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}
