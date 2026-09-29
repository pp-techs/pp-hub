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
// (shares_edit_download_ck); view is read access to the file's content, and
// can_download adds the right to take the bytes away.
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
// only hand out capabilities you hold yourself. The same rule bounds changing
// or removing an existing grant: a grant wider than what you hold is not
// yours to narrow or revoke, or a viewer allowed to re-share could strip an
// editor the owner appointed.
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

// Contains reports whether c holds every capability other does. A move is
// refused for a non-owner whose destination would give them more than they
// hold on the node now.
func (c Caps) Contains(other Caps) bool {
	return (!other.Owner || c.Owner) && (!other.Edit || c.Edit) &&
		(!other.Download || c.Download) && (!other.Share || c.Share) &&
		(!other.View || c.View)
}

// GrantCapsOf is the capability triple an existing share row carries.
func GrantCapsOf(sh store.Share) GrantCaps {
	return GrantCaps{Level: sh.Level, CanDownload: sh.CanDownload, CanShare: sh.CanShare}
}

// Live reports whether a share row still grants anything.
func Live(sh store.Share) bool {
	return !sh.ExpiresAt.Valid || sh.ExpiresAt.Time.After(time.Now())
}

type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// CreateLink generates a random token, stores its sha256, returns plaintext
// once. Links are view-level and never re-share (shares_link_ck): a visitor can
// read what the link covers, and the only choice is whether they may also take
// the bytes away.
func (s *Service) CreateLink(ctx context.Context, nodeID pgtype.UUID, canDownload bool, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, string, error) {
	token, err := newLinkToken()
	if err != nil {
		return store.Share{}, "", err
	}
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

// linkTokenAlphabet is base62: a share token ends up in a URL, so every
// character must be one that survives being copied, pasted and mailed.
const linkTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// linkTokenLength is 16 base62 characters, a little over 95 bits. Guessing one
// is out of reach even before the per-token rate limit, and the token is short
// enough to live in a link somebody reads aloud.
const linkTokenLength = 16

// newLinkToken draws a fresh link token from crypto/rand.
//
// 256 is not a multiple of 62, so the obvious `b % 62` would make the first
// eight characters of the alphabet likelier than the rest. Bytes at or above
// the largest multiple of 62 are discarded instead (248), which costs a few
// redraws and buys back an even distribution.
func newLinkToken() (string, error) {
	// 248 = 256 rounded down to a multiple of 62; bytes at or above it are
	// redrawn rather than folded in.
	const limit byte = 248
	out := make([]byte, linkTokenLength)
	buf := make([]byte, linkTokenLength)
	for i := 0; i < linkTokenLength; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out[i] = linkTokenAlphabet[int(b)%len(linkTokenAlphabet)]
			i++
			if i == linkTokenLength {
				break
			}
		}
	}
	return string(out), nil
}

// UpsertDirect grants a user access to a node, or replaces the grant they
// already hold on it (shares_direct_uniq): re-sharing is how a grant is
// narrowed or widened, never a second row beside the first.
func (s *Service) UpsertDirect(ctx context.Context, nodeID, granteeID pgtype.UUID, caps GrantCaps, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.Share, error) {
	if granteeID == creator {
		return store.Share{}, ErrSelfShare
	}
	caps = caps.Normalized()
	return s.q.UpsertDirectShare(ctx, store.UpsertDirectShareParams{
		NodeID:        nodeID,
		GranteeUserID: granteeID,
		Level:         caps.Level,
		CanDownload:   caps.CanDownload,
		CanShare:      caps.CanShare,
		ExpiresAt:     expiresAt,
		CreatedBy:     creator,
	})
}

// DirectFor returns the live direct grant a user holds on exactly this node.
func (s *Service) DirectFor(ctx context.Context, nodeID, granteeID pgtype.UUID) (store.Share, bool, error) {
	sh, err := s.q.GetDirectShare(ctx, store.GetDirectShareParams{NodeID: nodeID, GranteeUserID: granteeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Share{}, false, nil
	}
	return sh, err == nil, err
}

func (s *Service) ListForNode(ctx context.Context, nodeID pgtype.UUID) ([]store.Share, error) {
	return s.q.ListSharesForNode(ctx, nodeID)
}

// Get loads one share, only if it sits on nodeID — the node the request was
// authorised against. Anything else is ErrNotFound.
func (s *Service) Get(ctx context.Context, nodeID, shareID pgtype.UUID) (store.Share, error) {
	sh, err := s.q.GetShareOnNode(ctx, store.GetShareOnNodeParams{ID: shareID, NodeID: nodeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Share{}, ErrNotFound
	}
	return sh, err
}

// Revoke deletes a share, refusing one that does not belong to nodeID.
//
// Returns false when nothing matched, which the edge answers as 404 rather
// than a silent success: "revoked" and "that share was somebody else's" are
// different answers and only one of them is true.
func (s *Service) Revoke(ctx context.Context, nodeID, shareID pgtype.UUID) (bool, error) {
	n, err := s.q.DeleteShare(ctx, store.DeleteShareParams{ID: shareID, NodeID: nodeID})
	return n > 0, err
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
