// Package share implements direct and link sharing plus permission resolution.
// See docs/architecture/permissions.md.
package share

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	ErrNotFound     = errors.New("share not found")
	ErrInvalidToken = errors.New("invalid or expired share link")
	ErrUserNotFound = errors.New("grantee user not found")
	ErrSelfShare    = errors.New("cannot share with yourself")
)

// Perm is the permission a share grants. Order: view < download < edit.
type Perm = store.SharePerm

const (
	PermView     = store.SharePermView
	PermDownload = store.SharePermDownload
	PermEdit     = store.SharePermEdit
)

func permRank(p Perm) int {
	switch p {
	case PermEdit:
		return 3
	case PermDownload:
		return 2
	case PermView:
		return 1
	default:
		return 0
	}
}

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// CreateLink generates a random token, stores its sha256, returns plaintext once.
func (s *Service) CreateLink(ctx context.Context, nodeID pgtype.UUID, perm Perm, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return store.Share{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	sh, err := s.q.CreateShare(ctx, store.CreateShareParams{
		NodeID:    nodeID,
		Kind:      store.ShareKindLink,
		TokenHash: pgtype.Text{String: hex.EncodeToString(sum[:]), Valid: true},
		Perm:      perm,
		ExpiresAt: expiresAt,
		CreatedBy: creator,
	})
	return sh, token, err
}

// CreateDirect grants a user access to a node.
func (s *Service) CreateDirect(ctx context.Context, nodeID, granteeID pgtype.UUID, perm Perm, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, error) {
	if granteeID == creator {
		return store.Share{}, ErrSelfShare
	}
	return s.q.CreateShare(ctx, store.CreateShareParams{
		NodeID:        nodeID,
		Kind:          store.ShareKindDirect,
		GranteeUserID: granteeID,
		Perm:          perm,
		ExpiresAt:     expiresAt,
		CreatedBy:     creator,
	})
}

func (s *Service) ListForNode(ctx context.Context, nodeID pgtype.UUID) ([]store.Share, error) {
	return s.q.ListSharesForNode(ctx, nodeID)
}

func (s *Service) Revoke(ctx context.Context, shareID pgtype.UUID) error {
	return s.q.DeleteShare(ctx, shareID)
}

// ResolveLink validates a link token and returns the share row.
func (s *Service) ResolveLink(ctx context.Context, token string) (store.Share, error) {
	sum := sha256.Sum256([]byte(token))
	sh, err := s.q.GetShareByToken(ctx, pgtype.Text{String: hex.EncodeToString(sum[:]), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Share{}, ErrInvalidToken
	}
	return sh, err
}

// --- Permission resolution ---

// EffectivePerm computes the highest permission a user has on a node,
// combining space role with direct shares on the node or its ancestors.
// Returns rank 0 (none) when the user has no access at all.
//
// roleRank: viewer=1, editor=2, owner=3 (space roles)
// permRank: view=1, download=2, edit=3 (share perms)
// We treat role editor/owner as ≥ edit for node access purposes.
func (s *Service) EffectivePerm(ctx context.Context, node store.Node, userID pgtype.UUID, spaceRoleRank int) (int, error) {
	best := spaceRoleRank // viewer=1 → view, editor/owner=2/3 → download/edit
	if best > 3 {
		best = 3
	}

	// Collect ancestor chain (self + parents) for direct share lookup.
	ids := []pgtype.UUID{node.ID}
	cur := node.ParentID
	for depth := 0; depth < 100 && cur.Valid; depth++ {
		ids = append(ids, cur)
		next, err := s.q.GetNodeParent(ctx, cur)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return best, err
		}
		cur = next
	}

	shares, err := s.q.FindDirectSharesForUser(ctx, store.FindDirectSharesForUserParams{
		Column1:       ids,
		GranteeUserID: userID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return best, err
	}
	for _, sh := range shares {
		if r := permRank(sh.Perm); r > best {
			best = r
		}
	}
	return best, nil
}

// CanRead / CanWrite map the effective rank onto common checks.
func CanRead(rank int) bool  { return rank >= 1 }
func CanWrite(rank int) bool { return rank >= 3 }

// LinkPerm returns the read-path permission a link token grants (never edit).
func LinkPerm(sh store.Share) (int, error) {
	if sh.Kind != store.ShareKindLink {
		return 0, fmt.Errorf("not a link share")
	}
	r := permRank(sh.Perm)
	if r > permRank(PermDownload) {
		r = permRank(PermDownload) // clamp: links are read-only in MVP
	}
	return r, nil
}

// TokenExpiry builds an optional expiry timestamp from a duration string.
func TokenExpiry(d time.Duration) pgtype.Timestamptz {
	if d <= 0 {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: time.Now().Add(d), Valid: true}
}
