package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type shareDTO struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Kind        string `json:"kind"`
	Level       string `json:"level"`
	CanDownload bool   `json:"can_download"`
	CanShare    bool   `json:"can_share"`
	GranteeID   string `json:"grantee_user_id,omitempty"`
	GranteeName string `json:"grantee_name,omitempty"`
	Grantee     string `json:"grantee_email,omitempty"`
	URL         string `json:"url,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	// GranteeOwned counts live nodes inside this node that the grantee
	// created. Revoking does not touch them: ownership is intrinsic and has no
	// row to delete, so the dialog has to say what stays behind. Direct shares
	// only — a group grant has no single author to attribute.
	GranteeOwned int64 `json:"grantee_owned,omitempty"`
}

// withGrantee names a direct share's grantee on the DTO.
func (d shareDTO) withGrantee(u store.User) shareDTO {
	d.GranteeID = u.ID.String()
	d.GranteeName = displayName(u)
	d.Grantee = u.Email
	return d
}

func shareToDTO(sh store.Share, externalURL, token string) shareDTO {
	id, _ := sh.ID.Value()
	nid, _ := sh.NodeID.Value()
	d := shareDTO{
		ID:          id.(string),
		NodeID:      nid.(string),
		Kind:        string(sh.Kind),
		Level:       string(sh.Level),
		CanDownload: sh.CanDownload,
		CanShare:    sh.CanShare,
		CreatedAt:   sh.CreatedAt.Time.Format(time.RFC3339),
	}
	if sh.ExpiresAt.Valid {
		d.ExpiresAt = sh.ExpiresAt.Time.Format(time.RFC3339)
	}
	if token != "" {
		d.URL = externalURL + "/s/" + token
	}
	return d
}

// handleCreateShare creates a link, or creates or updates a direct grant.
//
// The wire carries the storage model directly — level plus the two flags —
// because a single ranked word cannot say "may view and pass on, but not
// download", which is exactly what the share dialog offers.
//
// A direct grant names its grantee by user id, never by address: the dialog
// picks from the space's member list, and an address lookup answered "no such
// user" and "not in this space" differently — an oracle for which addresses
// have accounts in other companies on the same instance.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	u, node, callerCaps := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	var body struct {
		Kind        string `json:"kind"`
		Level       string `json:"level"`
		CanDownload bool   `json:"can_download"`
		CanShare    bool   `json:"can_share"`
		UserID      string `json:"user_id"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	want, ok := grantCapsFromBody(body.Level, body.CanDownload, body.CanShare)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "level must be view|edit")
		return
	}
	// Monotonicity: a grantee who only holds download must not be able to mint
	// an editor. A link never carries edit, so it is checked against the
	// view/download half only.
	if !callerCaps.Covers(want) {
		writeProblem(w, http.StatusForbidden, "Forbidden", share.ErrEscalation.Error())
		return
	}
	var expires pgtype.Timestamptz
	if body.ExpiresIn > 0 {
		expires = share.TokenExpiry(time.Duration(body.ExpiresIn) * time.Second)
	}

	switch body.Kind {
	case "link":
		// Ownership is intrinsic and cannot be narrowed by a grant, so a
		// member who uploads into a colleague's folder owns that file and may
		// share it however they like — `can_share: false` on the folder does
		// not reach them. Whether "however they like" includes a public URL is
		// therefore a space-wide policy rather than a per-grant one.
		sp, err := s.store.GetSpace(r.Context(), node.SpaceID)
		if err != nil {
			Error(w, r, err)
			return
		}
		if !sp.AllowPublicLinks {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"public links are turned off for this space")
			return
		}
		sh, token, err := s.share.CreateLink(r.Context(), node.ID, want.CanDownload, expires, u.ID)
		if err != nil {
			Error(w, r, err)
			return
		}
		s.activity.Node(r.Context(), u.ID, node, activity.KindShared, map[string]any{
			"share_kind":   "link",
			"level":        string(sh.Level),
			"can_download": sh.CanDownload,
		})
		// A public link is the widest grant this app can make — anonymous,
		// bearer, and forwardable — so it belongs in the security log and not
		// only in the space's feed.
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindShareCreated,
			UserID:  u.ID,
			Subject: u.Email,
			NodeID:  node.ID,
			Detail: map[string]any{
				"node": node.ID.String(), "name": node.Name, "share_kind": "link",
				"level": string(sh.Level), "can_download": sh.CanDownload,
			},
		})
		writeJSON(w, http.StatusCreated, shareToDTO(sh, s.externalURL, token))
	case "direct":
		s.upsertDirectShare(w, r, u, node, callerCaps, body.UserID, want, expires)
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "kind must be link|direct")
	}
}

// upsertDirectShare creates a person's grant on the node or replaces the one
// they hold: 201 for a new grant, 200 for a change.
func (s *Server) upsertDirectShare(
	w http.ResponseWriter, r *http.Request,
	u *store.User, node store.Node, callerCaps share.Caps,
	rawUserID string, want share.GrantCaps, expires pgtype.Timestamptz,
) {
	ctx := r.Context()
	var granteeID pgtype.UUID
	if err := granteeID.Scan(rawUserID); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "user_id is required")
		return
	}
	if granteeID == u.ID {
		writeProblem(w, http.StatusBadRequest, "Bad Request", share.ErrSelfShare.Error())
		return
	}
	// One answer for "no such account" and "not in this space", or the
	// endpoint tells a member of one company who has an account in another.
	grantee, err := s.store.GetUser(ctx, granteeID)
	if err == nil {
		err = s.spaces.RequireMember(ctx, node.SpaceID, grantee.ID)
	}
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, spaces.ErrNotMember) {
			Error(w, r, err)
			return
		}
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable",
			"that person is not a member of this space — share a link instead")
		return
	}
	existing, had, err := s.share.DirectFor(ctx, node.ID, grantee.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if had {
		if !callerCaps.Covers(share.GrantCapsOf(existing)) {
			writeProblem(w, http.StatusForbidden, "Forbidden", errGrantWider)
			return
		}
		if !s.keepsTeamManager(w, r, node, existing.CanShare, want.CanShare) {
			return
		}
	}
	sh, err := s.share.UpsertDirect(ctx, node.ID, grantee.ID, want, expires, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	s.activity.Node(ctx, u.ID, node, activity.KindShared, map[string]any{
		"share_kind":   "direct",
		"level":        string(sh.Level),
		"can_download": sh.CanDownload,
		"can_share":    sh.CanShare,
		"grantee":      grantee.Email,
		"updated":      had,
	})
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindShareCreated,
		UserID:  u.ID,
		Subject: u.Email,
		NodeID:  node.ID,
		Detail: map[string]any{
			"node": node.ID.String(), "name": node.Name, "share_kind": "direct",
			"level": string(sh.Level), "can_download": sh.CanDownload, "can_share": sh.CanShare,
			"grantee": grantee.Email, "updated": had,
		},
	})
	status := http.StatusOK
	if !had {
		status = http.StatusCreated
		// A changed grant is not news worth an email; a new one is.
		if !grantee.SuspendedAt.Valid {
			s.notify.Shared(ctx, notify.Share{
				Granter:     *u,
				Node:        node,
				Level:       sh.Level,
				CanDownload: sh.CanDownload,
				Recipients: []notify.Recipient{{
					ID:       grantee.ID,
					Email:    grantee.Email,
					Settings: grantee.Settings,
				}},
			})
		}
	}
	writeJSON(w, status, shareToDTO(sh, s.externalURL, "").withGrantee(grantee))
}

// errGrantWider refuses changing or removing a grant broader than what the
// caller holds. Monotonicity bounds what you may hand out; it bounds what you
// may take away for the same reason — otherwise a viewer allowed to re-share
// could strip an editor the owner appointed.
const errGrantWider = "that grant is wider than your own access; only someone who holds at least as much can change or remove it"

// keepsTeamManager refuses a change that would leave a team folder with no
// manager — nobody holding can_share on it. Such a folder is a dead end: no
// person owns team content, so nobody could grant access or appoint anyone,
// and admins reach it only through a departure's re-seat. wasManager is
// whether the grant being changed is a live can_share grant; stillManager
// whether it remains one afterwards.
func (s *Server) keepsTeamManager(w http.ResponseWriter, r *http.Request, node store.Node, wasManager, stillManager bool) bool {
	if node.TeamRootID != node.ID || !wasManager || stillManager {
		return true
	}
	n, err := s.fs.TeamManagerCount(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return false
	}
	if n <= 1 {
		writeProblem(w, http.StatusConflict, "Conflict",
			"a team folder needs at least one manager — let someone else re-share it before removing this one")
		return false
	}
	return true
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	_, node, _ := s.requireNodeCaps(w, r, share.CapShare)
	if !node.ID.Valid {
		return
	}
	shares, err := s.share.ListForNode(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]shareDTO, 0, len(shares))
	for _, sh := range shares {
		d := shareToDTO(sh, s.externalURL, "")
		if sh.Kind == store.ShareKindDirect && sh.GranteeUserID.Valid {
			if g, err := s.store.GetUser(r.Context(), sh.GranteeUserID); err == nil {
				d = d.withGrantee(g)
			}
			// Inside a team folder the count would be a lie: revoking really
			// does remove access to what the grantee uploaded, because
			// `created_by` grants nothing there.
			if !node.TeamRootID.Valid {
				if n, err := s.fs.OwnedCount(r.Context(), node.ID, sh.GranteeUserID); err == nil {
					d.GranteeOwned = n
				}
			}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// inheritedAccessDTO is one principal reaching a node from above it.
type inheritedAccessDTO struct {
	Kind        string `json:"kind"` // owner | user | group | link
	PrincipalID string `json:"principal_id"`
	Name        string `json:"name"`
	ViaID       string `json:"via_id"`
	ViaName     string `json:"via_name"`
	Level       string `json:"level"`
	CanDownload bool   `json:"can_download"`
	CanShare    bool   `json:"can_share"`
}

// handleInheritedAccess answers the half of "who can open this" that the
// node's own share rows cannot: owners of the node or a folder above, and
// grants on those folders. Without it the share dialog listed only the rows
// on the node and implied nobody else reached it. Same gate as listing the
// node's shares.
func (s *Server) handleInheritedAccess(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	rows, err := s.fs.InheritedAccess(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]inheritedAccessDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, inheritedAccessDTO{
			Kind:        row.Kind,
			PrincipalID: row.PrincipalID.String(),
			Name:        row.Name,
			ViaID:       row.ViaID.String(),
			ViaName:     row.ViaName,
			Level:       string(row.Level),
			CanDownload: row.CanDownload,
			CanShare:    row.CanShare,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	u, node, callerCaps := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	var shareID pgtype.UUID
	if err := shareID.Scan(chi.URLParam(r, "shareID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid share id")
		return
	}
	existing, err := s.share.Get(r.Context(), node.ID, shareID)
	if errors.Is(err, share.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such share on this node")
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	if !callerCaps.Covers(share.GrantCapsOf(existing)) {
		writeProblem(w, http.StatusForbidden, "Forbidden", errGrantWider)
		return
	}
	if !s.keepsTeamManager(w, r, node, existing.CanShare && share.Live(existing), false) {
		return
	}
	ok, err := s.share.Revoke(r.Context(), node.ID, shareID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such share on this node")
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindUnshared, nil)
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindShareRevoked,
		UserID:  u.ID,
		Subject: u.Email,
		NodeID:  node.ID,
		Detail:  map[string]any{"node": node.ID.String(), "name": node.Name, "share": shareID.String()},
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- anonymous link access ---

func (s *Server) resolveLinkNode(w http.ResponseWriter, r *http.Request) (store.Share, store.Node) {
	token := chi.URLParam(r, "token")
	sh, err := s.share.ResolveLink(r.Context(), token)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "invalid or expired share link")
		return store.Share{}, store.Node{}
	}
	node, err := s.fs.Get(r.Context(), sh.NodeID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "file no longer exists")
		return store.Share{}, store.Node{}
	}
	// A link is only as alive as its author's own access.
	//
	// Resolution used to be the token hash and the expiry, nothing else — so
	// revoking the grant that let somebody share a folder left every link
	// they had minted working, anonymously, forever. The codebase already
	// held the opposite intent for the stronger action: removing a member
	// runs DeleteSharesByCreatorInSpace so that "their sharing decisions
	// leave with them". Revoking one grant is the weaker action and left the
	// bigger hole.
	//
	// Re-resolving here rather than cascading on revoke is the same choice
	// made for WOPI tokens: one query against present-day access covers
	// every way access can disappear, including an ancestor being moved,
	// rather than one cleanup path per way.
	caps, err := s.share.EffectiveCapsForNode(r.Context(), node, sh.CreatedBy)
	if err != nil {
		Error(w, r, err)
		return store.Share{}, store.Node{}
	}
	if !caps.Has(share.CapShare) {
		writeProblem(w, http.StatusNotFound, "Not Found",
			"this link is no longer valid: whoever created it no longer shares this file")
		return store.Share{}, store.Node{}
	}
	// Nor is it wider than their access now. Monotonicity was checked when
	// the link was minted; a creator since narrowed to view without download
	// must not keep handing out the bytes anonymously through a link they
	// made while they could. Clamping the row here reaches every download
	// route, since they all read the flag through LinkCaps.
	if !caps.Has(share.CapDownload) {
		sh.CanDownload = false
	}
	return sh, node
}

func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	sh, node := s.resolveLinkNode(w, r)
	if !sh.ID.Valid {
		return
	}
	caps, err := share.LinkCaps(sh)
	if err != nil || !caps.Download {
		writeProblem(w, http.StatusForbidden, "Forbidden", "this link does not allow downloads")
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileDownloaded, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: n})
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", n.Name))
	// The stored MIME comes from the extension the uploader chose, so it is
	// attacker-selected within Go's table. Every other byte-serving path here
	// sets this; the gap was the only one.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}
