// Package groups implements named sets of users inside a space and the grants
// that give those sets access to folders.
//
// See docs/architecture/permissions.md. A space is a company; a `member` role
// grants no file access on its own, so a group grant is how anyone other than
// an admin reaches anything.
package groups

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound = errors.New("group not found")
	// ErrEveryoneImmutable guards the automatic group. Its membership mirrors
	// space membership and is maintained by spaces.Service; letting it be
	// edited or deleted by hand would leave a space with no way for a new
	// member to reach anything.
	ErrEveryoneImmutable = errors.New("the Everyone group is maintained automatically")
	ErrNameTaken         = errors.New("a group with that name already exists in this space")
	ErrNameEmpty         = errors.New("group name is required")
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
	if g.IsEveryone {
		return store.Group{}, ErrEveryoneImmutable
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
	if g.IsEveryone {
		return ErrEveryoneImmutable
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
	if g.IsEveryone {
		return ErrEveryoneImmutable
	}
	return s.q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: groupID, UserID: userID})
}

func (s *Service) RemoveMember(ctx context.Context, groupID, userID pgtype.UUID) error {
	g, err := s.Get(ctx, groupID)
	if err != nil {
		return err
	}
	if g.IsEveryone {
		return ErrEveryoneImmutable
	}
	return s.q.RemoveGroupMember(ctx, store.RemoveGroupMemberParams{GroupID: groupID, UserID: userID})
}

// --- Grants ---

// Grant gives a group a permission on a node. node and group must share a
// space; otherwise a grant would punch a hole straight through the company
// boundary.
func (s *Service) Grant(ctx context.Context, node store.Node, groupID pgtype.UUID, perm store.SharePerm, createdBy pgtype.UUID) (store.Share, error) {
	g, err := s.Get(ctx, groupID)
	if err != nil {
		return store.Share{}, err
	}
	if g.SpaceID != node.SpaceID {
		return store.Share{}, ErrCrossSpace
	}
	return s.q.UpsertGroupGrant(ctx, store.UpsertGroupGrantParams{
		NodeID:         node.ID,
		Perm:           perm,
		GranteeGroupID: groupID,
		CreatedBy:      createdBy,
	})
}

func (s *Service) Revoke(ctx context.Context, nodeID, groupID pgtype.UUID) error {
	return s.q.DeleteGroupGrant(ctx, store.DeleteGroupGrantParams{
		NodeID:         nodeID,
		GranteeGroupID: groupID,
	})
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
