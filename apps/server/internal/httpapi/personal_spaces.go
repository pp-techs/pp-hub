package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Personal spaces and external sharing (docs/architecture/personal-spaces.md).

// refusePersonal answers 409 for administration a personal space does not
// have — members, groups, departures, team folders, linking, deletion — and
// reports whether it did.
func (s *Server) refusePersonal(w http.ResponseWriter, sp store.Space) bool {
	if !spaces.IsPersonal(sp) {
		return false
	}
	writeProblem(w, http.StatusConflict, "Conflict", spaces.ErrPersonal.Error())
	return true
}

// requireOrgSpaceAdmin is requireSpaceAdmin for the routes only an
// organization space has.
func (s *Server) requireOrgSpaceAdmin(w http.ResponseWriter, r *http.Request) (*store.User, store.Space, bool) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok || s.refusePersonal(w, space) {
		return nil, store.Space{}, false
	}
	return u, space, true
}

// requireOrgSpace loads a space by id and refuses a personal one.
func (s *Server) requireOrgSpace(w http.ResponseWriter, r *http.Request, spaceID pgtype.UUID) bool {
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return false
	}
	return !s.refusePersonal(w, space)
}

// handleListNodeChildren lists a folder by its own id. The space-addressed
// listing gates on membership first, which is right for a space's top level
// and wrong for somebody outside the space holding an external grant on a
// folder in it; this route takes view on the folder and nothing else, and
// access inherits downward, so the whole folder is visible.
func (s *Server) handleListNodeChildren(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusBadRequest, "Bad Request", fsNotDir)
		return
	}
	children, err := s.fs.ListWithMeta(r.Context(), node.SpaceID, node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(children))
	for _, n := range children {
		out = append(out, nodeToDTOMeta(n))
	}
	s.attachCaps(r.Context(), u.ID, out)
	writeJSON(w, http.StatusOK, out)
}

const fsNotDir = "not a folder"

type pathEntryDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// viewableChain is the node and its ancestors, nearest first, cut at the
// first one the caller cannot open. View inherits downward, so what they can
// open is an unbroken run from the node upward; nothing above it may be
// named to them.
func (s *Server) viewableChain(ctx context.Context, node store.Node, userID pgtype.UUID) ([]store.ListAncestryRow, error) {
	chain, err := s.store.ListAncestry(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]pgtype.UUID, len(chain))
	for i, c := range chain {
		ids[i] = c.ID
	}
	res, err := s.share.AccessCaps(ctx, ids, userID)
	if err != nil {
		return nil, err
	}
	reach := 0
	for reach < len(chain) && share.EffectiveCaps(res[chain[reach].ID]).View {
		reach++
	}
	return chain[:reach], nil
}

// handleNodePath is a breadcrumb: the node and its ancestors, from the
// topmost one the caller can open down to the node.
func (s *Server) handleNodePath(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	chain, err := s.viewableChain(r.Context(), node, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]pathEntryDTO, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		out = append(out, pathEntryDTO{ID: uuidString(chain[i].ID), Name: chain[i].Name})
	}
	writeJSON(w, http.StatusOK, out)
}

type nodePersonDTO struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// handleNodePeople is who a document's @-mention list offers. A member sees
// the space's roster plus whoever was let in from outside; an outsider sees
// only the people holding a direct grant on, or authoring, the part of the
// chain they can open — never the company's membership, nor who holds the
// folders above their share.
func (s *Server) handleNodePeople(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	chain, err := s.viewableChain(r.Context(), node, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	ids := make([]pgtype.UUID, len(chain))
	for i, c := range chain {
		ids[i] = c.ID
	}
	rows, err := s.store.ListNodePeople(r.Context(), store.ListNodePeopleParams{
		IncludeMembers: s.isMember(r.Context(), node.SpaceID, u.ID),
		SpaceID:        node.SpaceID,
		ChainIds:       ids,
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodePersonDTO, 0, len(rows))
	for _, p := range rows {
		name := p.DisplayName
		if name == "" {
			name = p.Name
		}
		if name == "" {
			name = p.Email
		}
		out = append(out, nodePersonDTO{
			UserID: uuidString(p.ID), Name: name, Email: p.Email, AvatarURL: p.AvatarUrl,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- shared from outside ---

// listExternalShared answers GET /shared?scope=external: the places the
// caller reaches through an external grant, in spaces they do not belong
// to. Each row names its space, which the caller cannot look up in their own
// space list.
func (s *Server) listExternalShared(w http.ResponseWriter, r *http.Request, u *store.User) {
	nodes, err := s.fs.ExternalEntryPointsWithMeta(r.Context(), u.ID, s.share.ExternalSharingEnabled())
	if err != nil {
		Error(w, r, err)
		return
	}
	names := map[pgtype.UUID]string{}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		dto := nodeToDTOMeta(n)
		name, ok := names[n.SpaceID]
		if !ok {
			name = s.spaceLabelForOutsider(r.Context(), n.SpaceID)
			names[n.SpaceID] = name
		}
		dto.SpaceName = name
		out = append(out, dto)
	}
	s.attachCaps(r.Context(), u.ID, out)
	writeJSON(w, http.StatusOK, out)
}

// spaceLabelForOutsider names a space to somebody outside it. A personal
// space's name is whatever its owner typed — "Acme Corp – Legal" as easily
// as their own name — so it is shown by its owner's account instead.
func (s *Server) spaceLabelForOutsider(ctx context.Context, spaceID pgtype.UUID) string {
	sp, err := s.spaces.Get(ctx, spaceID)
	if err != nil {
		return ""
	}
	if !spaces.IsPersonal(sp) {
		return sp.Name
	}
	owner, err := s.store.GetUser(ctx, sp.OwnerID)
	if err != nil {
		return ""
	}
	if owner.Email != "" {
		return displayName(owner) + " <" + owner.Email + ">"
	}
	return displayName(owner)
}

// --- pending shares ---

type pendingShareDTO struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Email       string `json:"email"`
	Level       string `json:"level"`
	CanDownload bool   `json:"can_download"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	// MailSent is set on the creation response only: false when no relay is
	// configured, so the sharer knows to pass the link on themselves.
	MailSent *bool `json:"mail_sent,omitempty"`
}

func pendingToDTO(p store.PendingShare) pendingShareDTO {
	d := pendingShareDTO{
		ID:          uuidString(p.ID),
		NodeID:      uuidString(p.NodeID),
		Email:       p.EmailNorm,
		Level:       string(p.Level),
		CanDownload: p.CanDownload,
		CreatedAt:   p.CreatedAt.Time.Format(time.RFC3339),
	}
	if p.ExpiresAt.Valid {
		d.ExpiresAt = p.ExpiresAt.Time.Format(time.RFC3339)
	}
	return d
}

// handleListPendingShares lists the invitations waiting on a node, under the
// same gate as its shares.
func (s *Server) handleListPendingShares(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	rows, err := s.share.ListPending(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]pendingShareDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, pendingToDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokePendingShare withdraws an invitation. The covering rule is the
// one a grant has: an invitation wider than the caller's own access is not
// theirs to withdraw.
func (s *Server) handleRevokePendingShare(w http.ResponseWriter, r *http.Request) {
	u, node, callerCaps := s.requireNodeCaps(w, r, share.CapShare)
	if u == nil {
		return
	}
	var pendingID pgtype.UUID
	if err := pendingID.Scan(chi.URLParam(r, "pendingID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid invitation id")
		return
	}
	p, err := s.share.GetPending(r.Context(), node.ID, pendingID)
	if errors.Is(err, share.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such invitation on this node")
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	if !callerCaps.Covers(share.GrantCaps{Level: p.Level, CanDownload: p.CanDownload}) {
		writeProblem(w, http.StatusForbidden, "Forbidden", errGrantWider)
		return
	}
	ok, err := s.share.RevokePending(r.Context(), node.ID, pendingID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such invitation on this node")
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindPendingShareRevoked,
		UserID:  u.ID,
		Subject: u.Email,
		NodeID:  node.ID,
		Detail:  map[string]any{"node": node.ID.String(), "name": node.Name, "invitee": p.EmailNorm},
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- archived personal spaces (instance admin) ---

type archivedPersonalDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OwnerEmail string `json:"owner_email"`
	UsedBytes  int64  `json:"used_bytes"`
	ArchivedAt string `json:"archived_at"`
	CreatedAt  string `json:"created_at"`
}

// handleAdminListArchivedPersonal lists the personal spaces whose owner Logto
// no longer has: unreachable by anyone, waiting for an instance admin.
func (s *Server) handleAdminListArchivedPersonal(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	rows, err := s.store.ListArchivedPersonalSpaces(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]archivedPersonalDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, archivedPersonalDTO{
			ID: uuidString(row.ID), Name: row.Name, OwnerEmail: row.OwnerEmail, UsedBytes: row.UsedBytes,
			ArchivedAt: row.ArchivedAt.Time.Format(timeLayout), CreatedAt: row.CreatedAt.Time.Format(timeLayout),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdminPurgePersonal deletes an archived personal space and everything
// in it. Only an archived one: a live personal space is its owner's, and an
// instance admin has no business destroying it.
func (s *Server) handleAdminPurgePersonal(w http.ResponseWriter, r *http.Request) {
	admin := s.requireAdmin(w, r)
	if admin == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	sp, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return
	}
	if !spaces.IsPersonal(sp) || !sp.ArchivedAt.Valid {
		writeProblem(w, http.StatusConflict, "Conflict", "only an archived personal space can be purged")
		return
	}
	if err := s.store.DeleteSpace(r.Context(), sp.ID); err != nil {
		Error(w, r, err)
		return
	}
	s.audit.Record(r, audit.Event{
		Kind:    audit.KindSettingsUpdated,
		UserID:  admin.ID,
		Subject: admin.Email,
		Detail:  map[string]any{"purged_personal_space": sp.ID.String(), "name": sp.Name},
	})
	w.WriteHeader(http.StatusNoContent)
}

// isMember reports whether the user belongs to the space; errors read as no.
func (s *Server) isMember(ctx context.Context, spaceID, userID pgtype.UUID) bool {
	return s.spaces.RequireMember(ctx, spaceID, userID) == nil
}
