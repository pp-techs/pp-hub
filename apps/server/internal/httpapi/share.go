package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

type shareDTO struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Kind        string `json:"kind"`
	Level       string `json:"level"`
	CanDownload bool   `json:"can_download"`
	CanShare    bool   `json:"can_share"`
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

// handleCreateShare creates a direct or link share on a node.
//
// The wire carries the storage model directly — level plus the two flags —
// because a single ranked word cannot say "may view and pass on, but not
// download", which is exactly what the share dialog offers.
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
		Email       string `json:"email"`
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
			Detail: map[string]any{
				"node": node.ID.String(), "name": node.Name, "share_kind": "link",
				"level": string(sh.Level), "can_download": sh.CanDownload,
			},
		})
		writeJSON(w, http.StatusCreated, shareToDTO(sh, s.externalURL, token))
	case "direct":
		grantee, err := s.store.GetUserByEmail(r.Context(), body.Email)
		if err != nil {
			writeProblem(w, http.StatusNotFound, "Not Found", "no user with that email")
			return
		}
		if err := s.spaces.RequireMember(r.Context(), node.SpaceID, grantee.ID); err != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable",
				"that person is not a member of this space — share a link instead")
			return
		}
		sh, err := s.share.CreateDirect(r.Context(), node.ID, grantee.ID, want, expires, u.ID)
		if err != nil {
			if errors.Is(err, share.ErrSelfShare) {
				writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
				return
			}
			Error(w, r, err)
			return
		}
		dto := shareToDTO(sh, s.externalURL, "")
		dto.Grantee = grantee.Email
		s.activity.Node(r.Context(), u.ID, node, activity.KindShared, map[string]any{
			"share_kind":   "direct",
			"level":        string(sh.Level),
			"can_download": sh.CanDownload,
			"can_share":    sh.CanShare,
			"grantee":      grantee.Email,
		})
		s.audit.Record(r, audit.Event{
			Kind:    audit.KindShareCreated,
			UserID:  u.ID,
			Subject: u.Email,
			Detail: map[string]any{
				"node": node.ID.String(), "name": node.Name, "share_kind": "direct",
				"level": string(sh.Level), "grantee": grantee.Email,
			},
		})
		if !grantee.SuspendedAt.Valid {
			s.notify.Shared(r.Context(), notify.Share{
				Granter:     *u,
				Node:        node,
				Level:       sh.Level,
				CanDownload: sh.CanDownload,
				Recipients: []notify.Recipient{
					{
						ID:       grantee.ID,
						Email:    grantee.Email,
						Settings: grantee.Settings,
					},
				},
			})
		}
		writeJSON(w, http.StatusCreated, dto)
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "kind must be link|direct")
	}
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
				d.Grantee = g.Email
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

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	var shareID pgtype.UUID
	if err := shareID.Scan(chi.URLParam(r, "shareID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid share id")
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
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", n.Name))
	// The stored MIME comes from the extension the uploader chose, so it is
	// attacker-selected within Go's table. Every other byte-serving path here
	// sets this; the gap was the only one.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}
