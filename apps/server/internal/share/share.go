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
	"strings"
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
	// externalEnabled reports the instance's external_sharing_enabled switch.
	// Nil means off, so a service built without sysconfig (tests, tools)
	// resolves no external grant at all.
	externalEnabled func() bool
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// SetExternalSharing wires the instance switch the resolver consults for
// external grants. Called once at startup, before any request.
func (s *Service) SetExternalSharing(enabled func() bool) {
	s.externalEnabled = enabled
}

// ExternalSharingEnabled reports the instance switch.
func (s *Service) ExternalSharingEnabled() bool {
	return s.externalEnabled != nil && s.externalEnabled()
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
//
// external marks a grantee outside the node's space. Such a grant never
// re-shares (shares_external_direct_ck), so CanShare is dropped here rather
// than left to fail at the constraint.
func (s *Service) UpsertDirect(ctx context.Context, nodeID, granteeID pgtype.UUID, caps GrantCaps, expiresAt pgtype.Timestamptz, creator pgtype.UUID, external bool) (store.Share, error) {
	if granteeID == creator {
		return store.Share{}, ErrSelfShare
	}
	caps = caps.Normalized()
	if external {
		caps.CanShare = false
	}
	return s.q.UpsertDirectShare(ctx, store.UpsertDirectShareParams{
		NodeID:        nodeID,
		GranteeUserID: granteeID,
		Level:         caps.Level,
		CanDownload:   caps.CanDownload,
		CanShare:      caps.CanShare,
		ExpiresAt:     expiresAt,
		CreatedBy:     creator,
		IsExternal:    external,
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

// --- Pending shares: invitations to addresses with no account ---

// InvitationsPerDay bounds how many people outside the spaces involved one
// person may give access to — external grants plus invitations — in any 24
// hours. With personal spaces open to everyone, the invitation mail is
// otherwise a free spam relay with this instance's name on it. Counted from
// invitation_log, which revoking does not shrink.
const InvitationsPerDay = 50

// InvitationMailsPerAddressPerDay caps how many invitation mails one address
// receives in any 24 hours, from everybody together. Past it the invitation
// is still recorded — it is adopted at sign-in like any other — but no mail
// goes out, so the instance cannot be pointed at somebody's inbox.
const InvitationMailsPerAddressPerDay = 3

// ErrTooManyInvitations is the rate limit's refusal.
var ErrTooManyInvitations = errors.New("too many invitations today — try again tomorrow")

// NormalizeEmail is the form pending shares are keyed and matched by.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func dayAgo() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
}

// CheckInvitationBudget refuses a new external grant or invitation once the
// creator has made InvitationsPerDay of them in the last 24 hours.
func (s *Service) CheckInvitationBudget(ctx context.Context, creator pgtype.UUID) error {
	n, err := s.q.CountInvitationsBySender(ctx, store.CountInvitationsBySenderParams{
		CreatedBy: creator, Since: dayAgo(),
	})
	if err != nil {
		return err
	}
	if n >= InvitationsPerDay {
		return ErrTooManyInvitations
	}
	return nil
}

// MayMailInvitation reports whether recipient is still under
// InvitationMailsPerAddressPerDay.
func (s *Service) MayMailInvitation(ctx context.Context, recipient string) (bool, error) {
	n, err := s.q.CountInvitationMailsTo(ctx, store.CountInvitationMailsToParams{
		Recipient: recipient, Since: dayAgo(),
	})
	return n < InvitationMailsPerAddressPerDay, err
}

// RecordInvitation appends to invitation_log: one external grant or
// invitation handed out, and whether a mail went with it.
func (s *Service) RecordInvitation(ctx context.Context, creator pgtype.UUID, recipient string, mailed bool) error {
	return s.q.RecordInvitation(ctx, store.RecordInvitationParams{
		CreatedBy: creator, Recipient: recipient, Mailed: mailed,
	})
}

// PruneInvitationLog drops entries older than any window counts.
func (s *Service) PruneInvitationLog(ctx context.Context) (int64, error) {
	return s.q.PruneInvitationLog(ctx, pgtype.Timestamptz{Time: time.Now().Add(-48 * time.Hour), Valid: true})
}

// UpsertPending invites an address with no account, or changes the
// invitation it already has on the node. inserted reports a new invitation.
// A pending share never re-shares: it becomes an external grant, or an
// ordinary one if its recipient turns out to be a member.
func (s *Service) UpsertPending(ctx context.Context, nodeID pgtype.UUID, emailNorm string, caps GrantCaps, expiresAt pgtype.Timestamptz, creator pgtype.UUID) (store.PendingShare, bool, error) {
	caps = caps.Normalized()
	row, err := s.q.UpsertPendingShare(ctx, store.UpsertPendingShareParams{
		NodeID:      nodeID,
		EmailNorm:   emailNorm,
		Level:       caps.Level,
		CanDownload: caps.CanDownload,
		CreatedBy:   creator,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return store.PendingShare{}, false, err
	}
	return store.PendingShare{
		ID: row.ID, TenantID: row.TenantID, NodeID: row.NodeID, EmailNorm: row.EmailNorm,
		Level: row.Level, CanDownload: row.CanDownload, CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
	}, row.Inserted, nil
}

// ListPending returns the live invitations on exactly this node.
func (s *Service) ListPending(ctx context.Context, nodeID pgtype.UUID) ([]store.PendingShare, error) {
	return s.q.ListPendingSharesForNode(ctx, nodeID)
}

// GetPending loads one invitation, only if it sits on nodeID.
func (s *Service) GetPending(ctx context.Context, nodeID, pendingID pgtype.UUID) (store.PendingShare, error) {
	p, err := s.q.GetPendingShareOnNode(ctx, store.GetPendingShareOnNodeParams{ID: pendingID, NodeID: nodeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.PendingShare{}, ErrNotFound
	}
	return p, err
}

// RevokePending withdraws an invitation; false when none matched on nodeID.
func (s *Service) RevokePending(ctx context.Context, nodeID, pendingID pgtype.UUID) (bool, error) {
	n, err := s.q.DeletePendingShare(ctx, store.DeletePendingShareParams{ID: pendingID, NodeID: nodeID})
	return n > 0, err
}

// Adopted is one invitation that became a grant.
type Adopted struct {
	Share     store.Share
	NodeName  string
	InvitedBy pgtype.UUID
}

// AdoptPending turns the invitations waiting for emailNorm into grants for
// user and deletes them all. Only call it with an address the identity
// provider has verified as the user's: the address is the whole of the claim.
//
// An invitation is the inviter's decision, deferred, so it is honoured only
// if the inviter could still make it now: they are still a member of the
// node's space, not suspended, and hold share on the node covering what they
// offered. Anything else — a departed or demoted inviter, an expiry passed —
// is dropped. A grant becomes external unless the user is already a member of
// the node's space, and a grant the user already holds is kept.
//
// viewOnly takes only the view invitations and leaves the others waiting —
// the call a guest session makes, because a guest can hold nothing wider and
// an edit invitation is for the address's own sign-in.
func (s *Service) AdoptPending(ctx context.Context, user store.User, emailNorm string, viewOnly bool) ([]Adopted, error) {
	if emailNorm == "" {
		return nil, nil
	}
	var out []Adopted
	err := s.q.InTx(ctx, func(q *store.Queries) error {
		tx := &Service{q: q, externalEnabled: s.externalEnabled}
		rows, err := q.TakePendingShares(ctx, store.TakePendingSharesParams{EmailNorm: emailNorm, ViewOnly: viewOnly})
		if err != nil {
			return err
		}
		for _, p := range rows {
			if p.CreatedBy == user.ID || (p.ExpiresAt.Valid && !p.ExpiresAt.Time.After(time.Now())) {
				continue
			}
			ok, err := tx.inviterStillMay(ctx, p)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			member, err := q.IsSpaceMember(ctx, store.IsSpaceMemberParams{SpaceID: p.SpaceID, UserID: user.ID})
			if err != nil {
				return err
			}
			sh, err := q.InsertAdoptedShare(ctx, store.InsertAdoptedShareParams{
				NodeID: p.NodeID, GranteeUserID: user.ID, Level: p.Level, CanDownload: p.CanDownload,
				ExpiresAt: p.ExpiresAt, CreatedBy: p.CreatedBy, IsExternal: !member,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			out = append(out, Adopted{Share: sh, NodeName: p.NodeName, InvitedBy: p.CreatedBy})
		}
		return nil
	})
	return out, err
}

func (s *Service) inviterStillMay(ctx context.Context, p store.TakePendingSharesRow) (bool, error) {
	inviter, err := s.q.GetUser(ctx, p.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || inviter.SuspendedAt.Valid {
		return false, err
	}
	member, err := s.q.IsSpaceMember(ctx, store.IsSpaceMemberParams{SpaceID: p.SpaceID, UserID: p.CreatedBy})
	if err != nil || !member {
		return false, err
	}
	res, err := s.AccessCaps(ctx, []pgtype.UUID{p.NodeID}, p.CreatedBy)
	if err != nil {
		return false, err
	}
	caps := EffectiveCaps(res[p.NodeID])
	return caps.Share && caps.Covers(GrantCaps{Level: p.Level, CanDownload: p.CanDownload}), nil
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
		NodeIds:         nodeIDs,
		UserID:          userID,
		ExternalEnabled: s.ExternalSharingEnabled(),
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

// ChildAccessCaps resolves children of one folder. Children of one folder
// share its ancestor chain, so the chain is resolved once, for the parent, and
// merged with each child's own grants and ownership. The result equals
// AccessCaps(childIDs).
func (s *Service) ChildAccessCaps(ctx context.Context, parentID pgtype.UUID, childIDs []pgtype.UUID, userID pgtype.UUID) (map[pgtype.UUID]Resolved, error) {
	out := make(map[pgtype.UUID]Resolved, len(childIDs))
	if len(childIDs) == 0 {
		return out, nil
	}
	parent, err := s.AccessCaps(ctx, []pgtype.UUID{parentID}, userID)
	if err != nil {
		return out, err
	}
	p := parent[parentID]
	rows, err := s.q.NodeOwnAccessCaps(ctx, store.NodeOwnAccessCapsParams{
		NodeIds:         childIDs,
		UserID:          userID,
		ExternalEnabled: s.ExternalSharingEnabled(),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	for _, row := range rows {
		out[row.NodeID] = Resolved{
			View:     p.View || row.View,
			Download: p.Download || row.Download,
			Share:    p.Share || row.Share,
			Edit:     p.Edit || row.Edit,
			Owner:    p.Owner || row.Owner,
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
