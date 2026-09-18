// Package share implements direct, group and link sharing plus permission
// resolution. See docs/architecture/permissions.md.
//
// M15 model: content access is ownership (nodes.created_by, inherited down
// the subtree) plus explicit grants. There is no role floor — an admin who
// owns nothing and was granted nothing resolves to no capabilities, exactly
// like anyone else.
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
	ErrEscalation   = errors.New("cannot grant capabilities you do not hold")
)

// Level is the tier a grant confers. edit implies view and download
// (shares_edit_download_ck); view is a preview unless can_download is set.
type Level = store.ShareLevel

const (
	LevelView = store.ShareLevelView
	LevelEdit = store.ShareLevelEdit
)

// GrantCaps is the input side of a grant: a level plus the two flags.
type GrantCaps struct {
	Level       Level
	CanDownload bool
	CanShare    bool
}

// Normalized enforces the edit-implies-download rule before insert.
func (g GrantCaps) Normalized() GrantCaps {
	if g.Level == LevelEdit {
		g.CanDownload = true
	}
	return g
}

// Cap identifies one capability for route gating.
type Cap int

const (
	CapView Cap = iota
	CapDownload
	CapShare
	CapEdit
)

// Has reports whether the set includes the capability.
func (c Caps) Has(cap Cap) bool {
	switch cap {
	case CapView:
		return c.View
	case CapDownload:
		return c.Download
	case CapShare:
		return c.Share
	case CapEdit:
		return c.Edit
	}
	return false
}

// Caps is the resolved set of capabilities a user holds on one node.
type Caps struct {
	View     bool `json:"view"`
	Download bool `json:"download"`
	Share    bool `json:"share"`
	Edit     bool `json:"edit"`
	// Owner: the user created this node or an ancestor. Owner is not a grant
	// and cannot be revoked — it is what makes self-lockout impossible.
	Owner bool `json:"owner"`
}

// CapsOwner is the full set ownership confers.
var CapsOwner = Caps{View: true, Download: true, Share: true, Edit: true, Owner: true}

// Covers reports whether c can grant want — the monotonicity rule: you can
// only hand out capabilities you hold yourself.
func (c Caps) Covers(want GrantCaps) bool {
	if want.Level == LevelEdit && !c.Edit {
		return false
	}
	if want.CanDownload && !c.Download {
		return false
	}
	if want.CanShare && !c.Share {
		return false
	}
	return true
}

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// CreateLink generates a random token, stores its sha256, returns plaintext
// once. Links are view-level and never re-share (shares_link_ck); the only
// choice is whether raw download is allowed.
func (s *Service) CreateLink(ctx context.Context, nodeID pgtype.UUID, canDownload bool, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return store.Share{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	sh, err := s.q.CreateShare(ctx, store.CreateShareParams{
		NodeID:      nodeID,
		Kind:        store.ShareKindLink,
		TokenHash:   pgtype.Text{String: hex.EncodeToString(sum[:]), Valid: true},
		Level:       LevelView,
		CanDownload: canDownload,
		CanShare:    false,
		ExpiresAt:   expiresAt,
		CreatedBy:   creator,
	})
	return sh, token, err
}

// CreateDirect grants a user access to a node.
func (s *Service) CreateDirect(ctx context.Context, nodeID, granteeID pgtype.UUID, caps GrantCaps, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, error) {
	if granteeID == creator {
		return store.Share{}, ErrSelfShare
	}
	caps = caps.Normalized()
	return s.q.CreateShare(ctx, store.CreateShareParams{
		NodeID:        nodeID,
		Kind:          store.ShareKindDirect,
		GranteeUserID: granteeID,
		Level:         caps.Level,
		CanDownload:   caps.CanDownload,
		CanShare:      caps.CanShare,
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

// Resolved is what AccessCaps knows about one node: the grant-derived
// capabilities, and whether the user owns the node or any ancestor.
type Resolved struct {
	View     bool
	Download bool
	Share    bool
	Edit     bool
	Owner    bool
}

// EffectiveCaps folds a Resolved row into the caller's capabilities: FULL for
// an owner, otherwise exactly what the grants give. Grants only ever widen;
// nothing narrows.
func EffectiveCaps(res Resolved) Caps {
	if res.Owner {
		return CapsOwner
	}
	edit := res.Edit
	return Caps{
		View:     res.View || edit,
		Download: res.Download || edit, // edit implies download, belt and braces
		Share:    res.Share,
		Edit:     edit,
	}
}

// EffectiveCapsForNode resolves one node. Callers with a batch of nodes should
// use AccessCaps once instead of looping this.
func (s *Service) EffectiveCapsForNode(ctx context.Context, node store.Node, userID pgtype.UUID) (Caps, error) {
	res, err := s.AccessCaps(ctx, []pgtype.UUID{node.ID}, userID)
	if err != nil {
		return Caps{}, err
	}
	return EffectiveCaps(res[node.ID]), nil
}

// AccessCaps resolves every requested node in one query, returning the
// grant-derived capabilities and ownership of each. Every requested node is
// present in the map — the owner flag must survive the no-grant case, since
// an owner typically holds no grant on their own files.
//
// One query covers every node and every ancestor chain, which is what lets a
// listing filter itself without a per-row permission check.
func (s *Service) AccessCaps(ctx context.Context, nodeIDs []pgtype.UUID, userID pgtype.UUID) (map[pgtype.UUID]Resolved, error) {
	out := make(map[pgtype.UUID]Resolved, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.NodeAccessCaps(ctx, store.NodeAccessCapsParams{
		NodeIds: nodeIDs,
		UserID:  userID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	for _, row := range rows {
		out[row.NodeID] = Resolved{
			View:     row.View,
			Download: row.Download,
			Share:    row.Share,
			Edit:     row.Edit,
			Owner:    row.Owner,
		}
	}
	return out, nil
}

// NodesWithShares reports which of the requested nodes carry at least one
// live share row directly on them — direct, group or link, any grantee. Used
// to badge a listing row "this is shared", which is a different question
// from AccessCaps: a node can be unreachable to the caller and still be
// shared with someone else, and a node the caller reaches via an ancestor's
// grant is not itself shared.
func (s *Service) NodesWithShares(ctx context.Context, nodeIDs []pgtype.UUID) (map[pgtype.UUID]bool, error) {
	out := make(map[pgtype.UUID]bool, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.NodesWithLiveShares(ctx, nodeIDs)
	if err != nil {
		return out, err
	}
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

// LinkCaps returns the capabilities a link token grants: view, and download
// only if the link allows it. Never edit, never share.
func LinkCaps(sh store.Share) (Caps, error) {
	if sh.Kind != store.ShareKindLink {
		return Caps{}, fmt.Errorf("not a link share")
	}
	return Caps{View: true, Download: sh.CanDownload}, nil
}

// TokenExpiry builds an optional expiry timestamp from a duration string.
func TokenExpiry(d time.Duration) pgtype.Timestamptz {
	if d <= 0 {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: time.Now().Add(d), Valid: true}
}
