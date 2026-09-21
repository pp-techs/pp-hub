package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// --- DTOs ---

type spaceDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OwnerID    string `json:"owner_id"`
	UsedBytes  int64  `json:"used_bytes"`
	QuotaBytes int64  `json:"quota_bytes"` // 0 = unlimited
	// IsAdmin: the caller is in the space's Admin group — the only thing
	// "admin" means anymore (M15).
	IsAdmin bool `json:"is_admin"`
	// AdminCount is how many people are in that group. It is on the wire
	// because a space can legitimately reach zero: the identity provider
	// owns membership now and can remove the last admin without asking, and
	// the only recovery is an instance admin. Every member therefore has to
	// be able to see the state, not just the administrators who no longer
	// exist.
	AdminCount int64 `json:"admin_count"`
	// IdpOrgID is the organization this space mirrors. Absent on an
	// unlinked space, which nothing reconciles and whose membership no
	// endpoint will change.
	IdpOrgID string `json:"idp_org_id,omitempty"`
	// ArchiveRootID is where departed members' content lands; admins browse
	// it there. Empty only on a space whose creation failed partway.
	ArchiveRootID string `json:"archive_root_id,omitempty"`
	CreatedAt     string `json:"created_at"`
	// Settings fields. These must be emitted even when empty: the settings
	// form reads the current values from here and PATCHes the whole set back.
	Slug               string `json:"slug"`
	Description        string `json:"description"`
	DefaultAsAdmin     bool   `json:"default_as_admin"`
	TrashRetentionDays int32  `json:"trash_retention_days"`
	// AllowPublicLinks gates anonymous share links for the whole space.
	// Ownership is intrinsic, so a member can always share what they created;
	// whether that may become a public URL is a company decision, not a
	// per-grant one.
	AllowPublicLinks bool `json:"allow_public_links"`
}

type nodeDTO struct {
	ID         string `json:"id"`
	SpaceID    string `json:"space_id"`
	ParentID   string `json:"parent_id,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Size       int64  `json:"size,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Version    int32  `json:"version,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	Owner      string `json:"owner,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
	CreatedAt  string `json:"created_at"`
	// Caps is the caller's resolved capability set on this node — what the
	// node menu enables. Set on listings and single-node GETs.
	Caps *share.Caps `json:"caps,omitempty"`
	// Shared reports whether this node itself carries a live share row —
	// direct, group or link, any grantee. It is not access: a node the
	// caller can only reach through an ancestor's grant is not itself
	// shared, and a node shared with someone else stays true even when the
	// caller holds no grant on it at all. Set alongside Caps.
	Shared bool `json:"shared,omitempty"`
	// ForeignOwned counts live nodes inside this folder that somebody other
	// than the caller created. Only set on single-folder GETs — a listing
	// would need one subtree walk per row — and it exists so a destructive
	// action can warn before it runs.
	ForeignOwned int64 `json:"foreign_owned,omitempty"`
	// TeamFolder marks a folder the team owns: `created_by` there is a byline,
	// access comes only from grants on it, and it outlives its creator.
	// InTeam marks anything inside one — the client uses it to stop offering
	// "owner" language for content that has no personal owner.
	TeamFolder bool `json:"team_folder,omitempty"`
	InTeam     bool `json:"in_team,omitempty"`
}

type versionDTO struct {
	ID        string `json:"id"`
	VersionNo int32  `json:"version_no"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	CreatedAt string `json:"created_at"`
}

type treeNodeDTO struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	HasChildren bool   `json:"has_children"`
}

func uuidParam(r *http.Request, name string) (pgtype.UUID, error) {
	var id pgtype.UUID
	return id, id.Scan(chi.URLParam(r, name))
}

func spaceToDTO(sp store.Space, isAdmin bool, adminCount int64) spaceDTO {
	return spaceDTO{
		ID:                 uuidString(sp.ID),
		Name:               sp.Name,
		OwnerID:            uuidString(sp.OwnerID),
		UsedBytes:          sp.UsedBytes,
		QuotaBytes:         sp.QuotaBytes,
		IsAdmin:            isAdmin,
		AdminCount:         adminCount,
		IdpOrgID:           sp.IdpOrgID.String,
		CreatedAt:          sp.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		Slug:               sp.Slug,
		Description:        sp.Description,
		DefaultAsAdmin:     sp.DefaultAsAdmin,
		TrashRetentionDays: sp.TrashRetentionDays,
		ArchiveRootID:      uuidString(sp.ArchiveRootID),
		AllowPublicLinks:   sp.AllowPublicLinks,
	}
}

// adminCount is the figure spaceToDTO needs and nothing else does. A failed
// count answers 0, which renders as "this space has no administrator" — the
// safe direction to be wrong in, since it prompts a human to look.
func (s *Server) adminCount(r *http.Request, spaceID pgtype.UUID) int64 {
	n, err := s.store.CountSpaceAdmins(r.Context(), spaceID)
	if err != nil {
		return 0
	}
	return int64(n)
}

func nodeToDTO(n store.Node, ver *store.NodeVersion) nodeDTO {
	id, _ := n.ID.Value()
	sid, _ := n.SpaceID.Value()
	dto := nodeDTO{
		ID:        id.(string),
		SpaceID:   sid.(string),
		Kind:      string(n.Kind),
		Name:      n.Name,
		CreatedAt: n.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if n.ParentID.Valid {
		if p, _ := n.ParentID.Value(); p != nil {
			dto.ParentID = p.(string)
		}
	}
	if ver != nil {
		dto.Size = ver.Size
		dto.Mime = ver.Mime
		dto.Version = ver.VersionNo
	}
	dto.TeamFolder = n.TeamRootID.Valid && n.TeamRootID == n.ID
	dto.InTeam = n.TeamRootID.Valid
	return dto
}

// withCaps stamps a DTO with capabilities the caller already knows without a
// query — creating a node makes you its owner, so the resolution is a constant.
// Every node DTO carries caps; a client should never have to guess at one.
func (d nodeDTO) withCaps(c share.Caps) nodeDTO {
	d.Caps = &c
	return d
}

func nodeToDTOMeta(n store.NodeWithMeta) nodeDTO {
	id, _ := n.ID.Value()
	sid, _ := n.SpaceID.Value()
	dto := nodeDTO{
		ID:        id.(string),
		SpaceID:   sid.(string),
		Kind:      string(n.Kind),
		Name:      n.Name,
		CreatedAt: n.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if n.ParentID.Valid {
		if p, _ := n.ParentID.Value(); p != nil {
			dto.ParentID = p.(string)
		}
	}
	if n.VerID.Valid {
		dto.Size = n.VerSize
		dto.Mime = n.VerMime
		dto.Version = n.VerVersionNo
		dto.ModifiedAt = n.VerCreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	if n.Kind == store.NodeKindDir && dto.ModifiedAt == "" {
		dto.ModifiedAt = n.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	if n.OwnerName != "" {
		dto.Owner = n.OwnerName
	} else if n.OwnerEmail != "" {
		dto.Owner = n.OwnerEmail
	}
	if n.OwnerID.Valid {
		if oid, _ := n.OwnerID.Value(); oid != nil {
			dto.OwnerID = oid.(string)
		}
	} else if n.CreatedBy.Valid {
		if oid, _ := n.CreatedBy.Value(); oid != nil {
			dto.OwnerID = oid.(string)
		}
	}
	dto.TeamFolder = n.TeamRootID.Valid && n.TeamRootID == n.ID
	dto.InTeam = n.TeamRootID.Valid
	return dto
}

// --- Spaces handlers ---

// canCreateSpaces reports whether this caller may mint a space by hand.
//
// The rule is one line because the decision is: if the identity provider
// owns who belongs to which company, then it owns which companies exist, and
// the only exception is the operator who has to be able to make an unlinked
// one. It is also on the profile DTO, so the client hides the action rather
// than offering a button that answers 403.
func (s *Server) canCreateSpaces(u *store.User) bool {
	return !s.orgs.Configured() || u.IsAdmin
}

// handleCreateSpace makes a space by hand.
//
// Once organizations own membership this is no longer an ordinary user's
// action. A space is a company; companies come from the identity provider,
// and a member who can mint one has made themselves its admin — which is
// exactly the authority the organization role was supposed to decide. The
// result is also a space nothing reconciles, so it drifts from the directory
// by construction.
//
// It stays open to an instance admin, who needs it for a local space with no
// organization behind it, and to everybody on an install with no Logto tenant
// configured at all — there, pp-hub is still the only place spaces exist.
func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if !s.canCreateSpaces(u) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"spaces come from identity-provider organizations on this server; ask an administrator to create the organization, and the space appears when its first member signs in")
		return
	}
	var body struct {
		Name           string `json:"name"`
		Slug           string `json:"slug"`
		Description    string `json:"description"`
		DefaultAsAdmin bool   `json:"default_as_admin"`
		QuotaBytes     int64  `json:"quota_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	sp, err := s.spaces.Create(r.Context(), strings.TrimSpace(body.Name), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	// Creation takes the same optional settings the settings screen edits.
	if body.Slug != "" || body.Description != "" || body.DefaultAsAdmin || body.QuotaBytes > 0 {
		settings, problem := spaceSettingsFrom(sp, body.Slug, body.Description, body.DefaultAsAdmin, body.QuotaBytes)
		if problem != "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request", problem)
			return
		}
		if updated, err := s.store.UpdateSpaceSettings(r.Context(), settings); err == nil {
			sp = updated
		} else if isUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "Conflict", "that slug is already taken")
			return
		} else {
			Error(w, r, err)
			return
		}
	}
	s.activity.Record(r.Context(), activity.Event{
		SpaceID: sp.ID, ActorID: u.ID, Kind: activity.KindCreated, NodeName: sp.Name,
	})
	writeJSON(w, http.StatusCreated, spaceToDTO(sp, true, 1)) // creator is the space's only admin
}

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	list, err := s.spaces.ListForUser(r.Context(), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]spaceDTO, 0, len(list))
	for _, sp := range list {
		out = append(out, spaceToDTO(sp, s.spaces.IsAdmin(r.Context(), sp.ID, u.ID), s.adminCount(r, sp.ID)))
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Node permission helpers ---

// requireParentWrite authorises creating something inside parentID.
//
// A parent in another space answers 404, not 403 — confirming that a node
// exists is already more than the caller should learn, the same rule anonymous
// share browsing follows.
//
// A nil parent means the space root, where any member may create: root items
// are private to their creator until shared (M15).
func (s *Server) requireParentWrite(w http.ResponseWriter, r *http.Request, spaceID, parentID pgtype.UUID, u *store.User) bool {
	if !parentID.Valid {
		if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
			s.spaceError(w, r, err)
			return false
		}
		return true
	}
	parent, err := s.fs.Get(r.Context(), parentID)
	if err != nil {
		s.nodeError(w, r, err)
		return false
	}
	if parent.SpaceID != spaceID {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such folder")
		return false
	}
	if parent.Kind != store.NodeKindDir {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "parent_id is not a folder")
		return false
	}
	caps, err := s.share.EffectiveCapsForNode(r.Context(), parent, u.ID)
	if err != nil {
		Error(w, r, err)
		return false
	}
	if !caps.Has(share.CapEdit) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return false
	}
	return true
}

// requireNodeCaps resolves the node and enforces that the caller holds the
// named capability on it. There is no role floor — an admin with neither
// ownership nor a grant resolves to nothing, like anyone else.
func (s *Server) requireNodeCaps(w http.ResponseWriter, r *http.Request, need share.Cap) (*store.User, store.Node, share.Caps) {
	u := caller(r)
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, store.Node{}, share.Caps{}
	}
	node, err := s.fs.Get(r.Context(), nodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return nil, store.Node{}, share.Caps{}
	}
	caps, err := s.share.EffectiveCapsForNode(r.Context(), node, u.ID)
	if err != nil {
		Error(w, r, err)
		return nil, store.Node{}, share.Caps{}
	}
	if !caps.Has(need) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return nil, store.Node{}, share.Caps{}
	}
	return u, node, caps
}

// attachCaps fills each DTO's Caps and Shared in one batched resolution.
func (s *Server) attachCaps(ctx context.Context, userID pgtype.UUID, nodes []nodeDTO) {
	if len(nodes) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(nodes))
	for _, n := range nodes {
		var id pgtype.UUID
		if err := id.Scan(n.ID); err == nil {
			ids = append(ids, id)
		}
	}
	res, err := s.share.AccessCaps(ctx, ids, userID)
	if err != nil {
		return // presentation detail
	}
	// A failure here only costs the badge, not access, so it does not abort
	// the caps resolution above.
	shared, err := s.share.NodesWithShares(ctx, ids)
	if err != nil {
		shared = nil
	}
	for i := range nodes {
		var id pgtype.UUID
		if err := id.Scan(nodes[i].ID); err != nil {
			continue
		}
		caps := share.EffectiveCaps(res[id])
		nodes[i].Caps = &caps
		nodes[i].Shared = shared[id]
	}
}

// isArchiveRoot reports whether the node is its space's archive root.
// The archive has a fixed place and cannot be renamed, moved or trashed.
func (s *Server) isArchiveRoot(ctx context.Context, node store.Node) bool {
	if node.Kind != store.NodeKindDir || node.ParentID.Valid {
		return false
	}
	sp, err := s.spaces.Get(ctx, node.SpaceID)
	return err == nil && sp.ArchiveRootID.Valid && sp.ArchiveRootID == node.ID
}

// --- Node handlers ---

func (s *Server) handleListChildren(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	var parentID pgtype.UUID
	if p := r.URL.Query().Get("parent"); p != "" {
		if err := parentID.Scan(p); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent id")
			return
		}
	}
	children, err := s.listVisibleChildren(r, spaceID, parentID, u)
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

// listVisibleChildren returns the children of parentID the caller may see.
//
// Access inherits downward: if the caller can view the parent they can view
// everything under it. Only when the parent is unreachable does the listing
// narrow to the children they own or hold a grant on directly.
//
// A nil parent is the space's top level: entry points — the caller's own items
// plus the places shared with them. Nobody, admin included, sees the whole tree.
func (s *Server) listVisibleChildren(r *http.Request, spaceID, parentID pgtype.UUID, u *store.User) ([]store.NodeWithMeta, error) {
	if !parentID.Valid {
		return s.fs.EntryPointsWithMeta(r.Context(), u.ID, spaceID, true)
	}
	parent, err := s.fs.Get(r.Context(), parentID)
	if err != nil {
		return nil, err
	}
	caps, err := s.share.EffectiveCapsForNode(r.Context(), parent, u.ID)
	if err != nil {
		return nil, err
	}
	if caps.Has(share.CapView) {
		return s.fs.ListWithMeta(r.Context(), spaceID, parentID)
	}
	return s.fs.ListGrantedWithMeta(r.Context(), spaceID, parentID, u.ID)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	var parentID pgtype.UUID
	if body.ParentID != "" {
		if err := parentID.Scan(body.ParentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}
	if !s.requireParentWrite(w, r, spaceID, parentID, u) {
		return
	}
	n, err := s.fs.Mkdir(r.Context(), spaceID, parentID, body.Name, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, n, activity.KindCreated, nil)
	writeJSON(w, http.StatusCreated, nodeToDTO(n, nil).withCaps(share.CapsOwner))
}

// handleCreateTeamFolder makes a folder the team owns rather than a person.
//
// Admin-only, deliberately. A team folder is a company-level container — it
// survives its creator's departure, it holds content nobody personally owns,
// and it is administered through grants — so creating one is space
// administration, which is the line `RequireAdmin` already draws. Members
// still create and share their own folders freely; that is the personal half
// of the tree.
//
// The creator gets the first manager grant (edit + download + share). Without
// it the folder would be unreachable the moment it existed, since a team
// folder grants nothing through ownership.
func (s *Server) handleCreateTeamFolder(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	node, err := s.fs.CreateTeamFolder(r.Context(), space.ID, strings.TrimSpace(body.Name), u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if _, err := s.store.CreateShare(r.Context(), store.CreateShareParams{
		NodeID:        node.ID,
		Kind:          store.ShareKindDirect,
		GranteeUserID: u.ID,
		Level:         store.ShareLevelEdit,
		CanDownload:   true,
		CanShare:      true,
		CreatedBy:     u.ID,
	}); err != nil {
		Error(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindCreated, map[string]any{"team_folder": true})
	dto := nodeToDTO(node, nil).withCaps(share.Caps{View: true, Download: true, Share: true, Edit: true})
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	if !parseUpload(w, r, s.maxUploadBytes) {
		return
	}
	var parentID pgtype.UUID
	if p := r.FormValue("parent_id"); p != "" {
		if err := parentID.Scan(p); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}
	if !s.requireParentWrite(w, r, spaceID, parentID, u) {
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "file field required")
		return
	}
	defer f.Close()

	node, ver, err := s.fs.PutFile(r.Context(), spaceID, parentID, pgtype.UUID{}, hdr.Filename, f, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindUploaded, map[string]any{"size": ver.Size})
	// Recount used bytes for the space.
	if used, err := s.store.SumSpaceUsage(r.Context(), spaceID); err == nil {
		_ = s.store.UpdateSpaceUsedBytes(r.Context(), store.UpdateSpaceUsedBytesParams{ID: spaceID, UsedBytes: used})
	}
	writeJSON(w, http.StatusCreated, nodeToDTO(node, &ver).withCaps(share.CapsOwner))
}

// lockedByEditor answers 409 when a live WOPI lock exists, and reports whether
// the caller should stop. Both REST write paths that change which bytes are
// current have to ask; the WOPI paths already do, with their own header
// protocol (X-WOPI-Lock), which is why this is not shared with them.
func (s *Server) lockedByEditor(w http.ResponseWriter, r *http.Request, nodeID pgtype.UUID) bool {
	lockToken, err := s.wopi.CurrentLock(r.Context(), nodeID)
	if err != nil {
		Error(w, r, err)
		return true
	}
	if lockToken != "" {
		writeProblem(w, http.StatusConflict, "Conflict",
			"this file is open in the editor; close it or wait for the lock to expire")
		return true
	}
	return false
}

// handleUploadVersion replaces the contents of an existing file. Requires
// CapEdit on that file.
//
// This is the only way to add a version through the API now: uploading into a
// folder never overwrites a file that happens to share its name, so replacing
// somebody's work has to be asked for by node id rather than stumbled into by
// filename. The node keeps its name and its owner — a new version is authored
// by whoever uploaded it (node_versions.created_by), which is what the version
// history shows.
func (s *Server) handleUploadVersion(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	// A .docx open in OnlyOffice is a lock row with a 30-minute TTL, and the
	// WOPI save path honours it. This path did not, so B's upload became A's
	// next save's base and A's work was gone with no conflict anywhere.
	//
	// Refused rather than locked: there is no unlock on this path — no
	// session to end, no REFRESH_LOCK — so a lock taken here would only be
	// released by the sweep.
	if s.lockedByEditor(w, r, node.ID) {
		return
	}
	if !parseUpload(w, r, s.maxUploadBytes) {
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "file field required")
		return
	}
	defer f.Close()

	updated, ver, err := s.fs.PutFile(r.Context(), node.SpaceID, node.ParentID, node.ID, node.Name, f, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, updated, activity.KindUploaded, map[string]any{
		"size":    ver.Size,
		"version": ver.VersionNo,
	})
	if used, err := s.store.SumSpaceUsage(r.Context(), node.SpaceID); err == nil {
		_ = s.store.UpdateSpaceUsedBytes(r.Context(), store.UpdateSpaceUsedBytesParams{ID: node.SpaceID, UsedBytes: used})
	}
	writeJSON(w, http.StatusOK, nodeToDTO(updated, &ver).withCaps(caps))
}

// inlineMimes are the types /content may hand the browser to render in place.
var inlineMimes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true,
	"image/gif": true, "image/webp": true, "video/mp4": true,
	"video/webm": true, "video/quicktime": true, "audio/mpeg": true,
	"audio/mp4": true, "audio/ogg": true, "audio/wav": true,
	"text/plain": true,
}

func inlineDisposition(mimeType string) string {
	base, _, _ := strings.Cut(mimeType, ";")
	if inlineMimes[strings.TrimSpace(strings.ToLower(base))] {
		return "inline"
	}
	return "attachment"
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	_, node, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if !node.ID.Valid {
		return
	}
	if node.Kind == store.NodeKindDir {
		s.streamZip(w, r, node)
		return
	}
	s.serveBlob(w, r, "attachment", share.CapDownload)
}

// handleContent serves the file's own bytes for the browser to render in
// place — the PDF viewer, an <img>, a <video>.
//
// It takes the download capability, not view. The bytes are the file: a
// reader who was refused download could otherwise open this URL in a tab and
// keep the original, which would make "view without download" a label rather
// than a rule. What such a reader still gets is the derived preview served by
// /preview — a thumbnail or a text snippet, generated from the file but not
// the file. Drive draws the line in the same place.
func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, "inline", share.CapDownload)
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, disposition string, need share.Cap) {
	_, node, _ := s.requireNodeCaps(w, r, need)
	if !node.ID.Valid {
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()

	if disposition == "inline" {
		disposition = inlineDisposition(ver.Mime)
	}
	cd := mime.FormatMediaType(disposition, map[string]string{"filename": n.Name})
	if cd == "" {
		cd = disposition
	}
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", cd)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, n.Name, ver.CreatedAt.Time, rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}

// requireMovableNode authorises moving, renaming or trashing a node, and hands
// back the caller's capabilities so the caller can apply the extra rules a
// move has. The archive root itself cannot be moved, renamed or trashed.
//
// A team folder is the team's, not its creator's, so renaming or trashing one
// takes `CapShare` — a manager of that folder — rather than the `CapEdit` that
// any contributor holds. Drive draws the same line: only a shared drive's
// managers can rename or delete it.
func (s *Server) requireMovableNode(w http.ResponseWriter, r *http.Request) (*store.User, store.Node, share.Caps) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapEdit)
	if !node.ID.Valid {
		return nil, store.Node{}, share.Caps{}
	}
	if s.isArchiveRoot(r.Context(), node) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "the archive has a fixed place and cannot be moved, renamed or trashed")
		return nil, store.Node{}, share.Caps{}
	}
	if node.TeamRootID == node.ID && !caps.Has(share.CapShare) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"only a manager of this team folder can rename or delete it")
		return nil, store.Node{}, share.Caps{}
	}
	return u, node, caps
}

// handleMoveImpact answers "who gains and who loses access if this node moves
// there" before the move happens. Same precondition as the move itself:
// CapEdit on the node and on the destination.
//
// Moving a file out of somebody else's folder is a legitimate act — it is how
// a member takes back something they created and Drive documents it as the way
// to narrow sharing — so this does not gate the move. It only makes the
// consequence visible, in both directions: moving INTO a shared folder hands
// the file to everyone that folder reaches.
func (s *Server) handleMoveImpact(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	var destID pgtype.UUID
	if p := r.URL.Query().Get("parent"); p != "" {
		if err := destID.Scan(p); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent")
			return
		}
	}
	if !s.requireParentWrite(w, r, node.SpaceID, destID, u) {
		return
	}
	impact, err := s.fs.MoveImpact(r.Context(), node, destID)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, moveImpactDTO{
		Losing:  principalDTOs(impact.Losing, u.ID),
		Gaining: principalDTOs(impact.Gaining, u.ID),
	})
}

type moveImpactDTO struct {
	Losing  []principalDTO `json:"losing"`
	Gaining []principalDTO `json:"gaining"`
}

type principalDTO struct {
	Kind string `json:"kind"` // user | group | link
	Name string `json:"name"`
}

// principalDTOs drops the caller: they are performing the move, and telling
// them they are about to lose their own access reads as a bug.
func principalDTOs(ps []fs.Principal, caller pgtype.UUID) []principalDTO {
	out := make([]principalDTO, 0, len(ps))
	for _, p := range ps {
		if p.Kind == "user" && p.ID == caller {
			continue
		}
		out = append(out, principalDTO{Kind: p.Kind, Name: p.Name})
	}
	return out
}

func (s *Server) handleRenameNode(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireMovableNode(w, r)
	if u == nil {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if err := s.fs.Rename(r.Context(), node.ID, body.Name); err != nil {
		s.nodeError(w, r, err)
		return
	}
	renamed := node
	renamed.Name = body.Name
	s.activity.Node(r.Context(), u.ID, renamed, activity.KindRenamed, map[string]any{"from": node.Name})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMoveNode relocates a node.
//
// Taking content OUT of a team folder needs `CapShare` on it — a manager of
// that folder — because a move out is an export: the subtree leaves the
// container, stops being the team's, and becomes the mover's personally owned
// content. Drive requires Manager access to move items out of a shared drive
// for exactly that reason. Moving deeper inside the same team folder, or into
// one, needs nothing beyond the write access the destination already demands.
func (s *Server) handleMoveNode(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireMovableNode(w, r)
	if u == nil {
		return
	}
	var body struct {
		ParentID string `json:"parent_id"` // empty = space root
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	var parentID pgtype.UUID
	if body.ParentID != "" {
		if err := parentID.Scan(body.ParentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}
	if !s.requireParentWrite(w, r, node.SpaceID, parentID, u) {
		return
	}
	if leavesTeam, err := s.movesOutOfTeam(r.Context(), node, parentID); err != nil {
		Error(w, r, err)
		return
	} else if leavesTeam && !caps.Has(share.CapShare) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"only a manager of this team folder can move content out of it")
		return
	}
	if err := s.fs.Move(r.Context(), node.ID, parentID, u.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindMoved, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// movesOutOfTeam reports whether the move would take the node out of the team
// folder it currently belongs to. Moving from one team folder to another counts
// as leaving the first.
func (s *Server) movesOutOfTeam(ctx context.Context, node store.Node, destID pgtype.UUID) (bool, error) {
	if !node.TeamRootID.Valid {
		return false, nil
	}
	if !destID.Valid {
		return true, nil
	}
	dest, err := s.fs.Get(ctx, destID)
	if err != nil {
		return false, err
	}
	return dest.TeamRootID != node.TeamRootID, nil
}

// handleCopyNode duplicates a node into a destination folder.
//
// The source is read, not written, so download is the capability it takes —
// the same one that already lets the caller pull the bytes down and upload
// them back. Requiring edit on the source instead would deny a reader the
// ordinary way to work from somebody else's file. The destination is a write
// and is checked as one, below, so a copy never lands anywhere the caller
// could not have uploaded to.
func (s *Server) handleCopyNode(w http.ResponseWriter, r *http.Request) {
	u, _, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if u == nil {
		return
	}
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return
	}
	node, err := s.fs.Get(r.Context(), nodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	var body struct {
		ParentID string `json:"parent_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	dest := node.ParentID
	if body.ParentID != "" {
		if err := dest.Scan(body.ParentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}
	if !s.requireParentWrite(w, r, node.SpaceID, dest, u) {
		return
	}
	copied, err := s.fs.Copy(r.Context(), node.ID, dest, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, copied, activity.KindCreated, map[string]any{"copied_from": node.Name})
	if used, err := s.store.SumSpaceUsage(r.Context(), node.SpaceID); err == nil {
		_ = s.store.UpdateSpaceUsedBytes(r.Context(), store.UpdateSpaceUsedBytesParams{ID: node.SpaceID, UsedBytes: used})
	}
	writeJSON(w, http.StatusCreated, nodeToDTO(copied, nil).withCaps(share.CapsOwner))
}
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireMovableNode(w, r)
	if u == nil {
		return
	}
	if err := s.fs.Delete(r.Context(), node.ID, u.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindTrashed, nil)
	if used, err := s.store.SumSpaceUsage(r.Context(), node.SpaceID); err == nil {
		_ = s.store.UpdateSpaceUsedBytes(r.Context(), store.UpdateSpaceUsedBytesParams{ID: node.SpaceID, UsedBytes: used})
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	var ver *store.NodeVersion
	if node.Kind == store.NodeKindFile {
		if v, err := s.store.GetCurrentVersion(r.Context(), node.ID); err == nil {
			ver = &v
		}
	}
	dto := nodeToDTO(node, ver)
	if u, err := s.store.GetUser(r.Context(), node.CreatedBy); err == nil {
		dto.Owner = displayName(u)
		if id, _ := u.ID.Value(); id != nil {
			dto.OwnerID = id.(string)
		}
	}
	dto.Caps = &caps
	// Inside a team folder "items other people created" is not a category:
	// the container owns everything, so a manager trashing a folder is not
	// reaching into anybody's personal work and the warning would be noise.
	if node.Kind == store.NodeKindDir && !node.TeamRootID.Valid {
		if n, err := s.fs.ForeignOwnedCount(r.Context(), node.ID, u.ID); err == nil {
			dto.ForeignOwned = n
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	_, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if !node.ID.Valid {
		return
	}
	vers, err := s.fs.Versions(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]versionDTO, 0, len(vers))
	for _, v := range vers {
		id, _ := v.ID.Value()
		out = append(out, versionDTO{
			ID:        id.(string),
			VersionNo: v.VersionNo,
			Size:      v.Size,
			Mime:      v.Mime,
			CreatedAt: v.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireMovableNode(w, r)
	if u == nil {
		return
	}
	var versionID pgtype.UUID
	if err := versionID.Scan(chi.URLParam(r, "versionID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid version id")
		return
	}
	if s.lockedByEditor(w, r, node.ID) {
		return
	}
	if err := s.fs.RestoreVersion(r.Context(), node.ID, versionID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindReverted, nil)
	if used, err := s.store.SumSpaceUsage(r.Context(), node.SpaceID); err == nil {
		_ = s.store.UpdateSpaceUsedBytes(r.Context(), store.UpdateSpaceUsedBytesParams{ID: node.SpaceID, UsedBytes: used})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Sidebar handlers ---

func (s *Server) handleListTree(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	var parentID pgtype.UUID
	if p := r.URL.Query().Get("parent"); p != "" {
		if err := parentID.Scan(p); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent id")
			return
		}
	}
	withMeta, err := s.listVisibleChildren(r, spaceID, parentID, u)
	if err != nil {
		Error(w, r, err)
		return
	}
	nodes := make([]store.Node, 0, len(withMeta))
	for _, n := range withMeta {
		nodes = append(nodes, n.Node)
	}
	out := make([]treeNodeDTO, 0, len(nodes))
	for _, n := range nodes {
		var parentStr string
		if n.ParentID.Valid {
			if p, _ := n.ParentID.Value(); p != nil {
				parentStr = p.(string)
			}
		}
		hasChildren := false
		if n.Kind == store.NodeKindDir {
			count, err := s.fs.HasChildren(r.Context(), n.ID)
			if err == nil {
				hasChildren = count > 0
			}
		}
		id, _ := n.ID.Value()
		out = append(out, treeNodeDTO{
			ID:          id.(string),
			ParentID:    parentStr,
			Kind:        string(n.Kind),
			Name:        n.Name,
			HasChildren: hasChildren,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListDeleted(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	all, err := s.fs.ListDeletedWithMeta(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	// Filter by reachability: the ancestor walk does not filter deleted_at, so
	// a deleted file inside a deleted folder still resolves against the grant
	// above it.
	rows, _, err := reachableRows(r.Context(), s, u.ID, all,
		func(n store.ListDeletedWithMetaRow) pgtype.UUID { return n.ID },
		func(n store.ListDeletedWithMetaRow) pgtype.UUID { return n.SpaceID })
	if err != nil {
		Error(w, r, err)
		return
	}
	// A folder somebody else trashed may bury work the caller created. They
	// hold no capability on that folder, so reachability drops it — and the
	// caller is left with files that have silently vanished and a purge they
	// cannot see coming. Those tombstones come back as read-only rows.
	buried, err := s.fs.TrashRootsWithUserContent(r.Context(), spaceID, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	visible := make(map[pgtype.UUID]bool, len(rows))
	for _, n := range rows {
		visible[n.ID] = true
	}
	out := make([]trashNodeDTO, 0, len(rows)+len(buried))
	for _, n := range rows {
		out = append(out, trashNodeDTO{
			ID:         uuidString(n.ID),
			SpaceID:    uuidString(n.SpaceID),
			Kind:       string(n.Kind),
			Name:       n.Name,
			ModifiedAt: n.VCreatedAt.Time.Format(timeLayout),
			DeletedAt:  n.DeletedAt.Time.Format(timeLayout),
			PurgesAt:   n.PurgesAt.Time.Format(timeLayout),
			BuriedOwn:  buried[n.ID],
		})
	}
	for _, n := range all {
		if visible[n.ID] || buried[n.ID] == 0 {
			continue
		}
		out = append(out, trashNodeDTO{
			ID:         uuidString(n.ID),
			SpaceID:    uuidString(n.SpaceID),
			Kind:       string(n.Kind),
			Name:       n.Name,
			ModifiedAt: n.VCreatedAt.Time.Format(timeLayout),
			DeletedAt:  n.DeletedAt.Time.Format(timeLayout),
			PurgesAt:   n.PurgesAt.Time.Format(timeLayout),
			BuriedOwn:  buried[n.ID],
			ReadOnly:   true,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListShared answers GET /api/v1/shared[?space=]. The entry-point query
// already carries an optional space filter, and it applies a LIMIT, so the
// scoping has to happen in SQL rather than on the assembled slice.
func (s *Server) handleListShared(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, ok := s.optionalSpaceParam(w, r, u.ID)
	if !ok {
		return
	}
	nodes, err := s.fs.ListShared(r.Context(), u.ID, spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeToDTOMeta(n))
	}
	s.attachCaps(r.Context(), u.ID, out)
	writeJSON(w, http.StatusOK, out)
}

// --- error mapping helpers ---

func (s *Server) nodeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, fs.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "node not found")
	case errors.Is(err, fs.ErrNameConflict):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, fs.ErrCycle), errors.Is(err, fs.ErrTeamRootFixed):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, fs.ErrNotDeleted):
		// Restoring or purging something that is not in the trash: a double
		// click, or somebody else restored it first. Not a server fault.
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, fs.ErrQuotaExceeded):
		writeProblem(w, http.StatusRequestEntityTooLarge, "Quota Exceeded", err.Error())
	case errors.Is(err, fs.ErrNotFile), errors.Is(err, fs.ErrNotDir):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		Error(w, r, err)
	}
}

func (s *Server) spaceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, spaces.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "space not found")
	case errors.Is(err, spaces.ErrNotMember), errors.Is(err, spaces.ErrForbidden):
		writeProblem(w, http.StatusForbidden, "Forbidden", err.Error())
	case errors.Is(err, spaces.ErrLastAdmin), errors.Is(err, spaces.ErrMemberExists):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, orgsync.ErrNotLinked):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable Entity",
			"this space mirrors no identity-provider organization, so its membership cannot be changed here")
	case errors.Is(err, orgsync.ErrDisabled):
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable",
			"this server holds no management credentials for the identity provider, so membership can only be changed there; it reaches pp-hub when the person next signs in")
	case errors.Is(err, orgsync.ErrUnreachable):
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable",
			"the identity provider is unreachable, so membership cannot be changed right now")
	case orgsync.Status(err) != 0:
		// Upstream said no. Its status is the honest one to relay — a flat
		// 500 would send an operator looking through this server's logs for
		// a fault that is in their Logto tenant.
		writeProblem(w, http.StatusBadGateway, "Bad Gateway",
			"the identity provider rejected the change: "+err.Error())
	default:
		Error(w, r, err)
	}
}
