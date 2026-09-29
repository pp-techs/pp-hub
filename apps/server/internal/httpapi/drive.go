package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/office"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
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

// canCreateSpaces reports whether this caller may create a space.
//
// A space is a company, and companies are Logto organizations: creating one
// here creates the organization upstream (handleCreateSpace), which is an
// instance administrator's act, not a member's. It is also on the profile
// DTO, so the client hides the action rather than offering a 403.
func (s *Server) canCreateSpaces(u *store.User) bool {
	return u.IsAdmin
}

// handleCreateSpace creates a company: a Logto organization, with the caller
// seated in it as admin, mirrored as a space (orgsync.CreateSpace).
//
// It is an instance administrator's action. A member who could mint a space
// would have made themselves the admin of a company, which is exactly the
// authority the organization role is supposed to decide.
func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if !s.canCreateSpaces(u) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"spaces are identity-provider organizations; ask an administrator to create one")
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
	sp, err := s.orgs.CreateSpace(r.Context(), *u, strings.TrimSpace(body.Name))
	if err != nil {
		s.identityProviderError(w, r, err, "spaces")
		return
	}
	// Creation takes the same optional settings the settings screen edits.
	quota := body.QuotaBytes
	if quota <= 0 && s.sysconfig != nil {
		quota = s.sysconfig.DefaultSpaceQuotaBytes("default")
	}
	if body.Slug != "" || body.Description != "" || body.DefaultAsAdmin || quota > 0 {
		settings, problem := spaceSettingsFrom(sp, body.Slug, body.Description, body.DefaultAsAdmin, quota)
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
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, store.Node{}, share.Caps{}
	}
	return s.requireNodeCapsByID(w, r, nodeID, need)
}

// requireNodeCapsByID is requireNodeCaps for a node named somewhere other
// than the URL — an upload's target, carried in its metadata.
func (s *Server) requireNodeCapsByID(w http.ResponseWriter, r *http.Request, nodeID pgtype.UUID, need share.Cap) (*store.User, store.Node, share.Caps) {
	u := caller(r)
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
// The creator gets the first manager grant (edit + download + share), written
// by fs.CreateTeamFolder in the same transaction as the folder. Without it the
// folder would be unreachable the moment it existed, since a team folder
// grants nothing through ownership.
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
	s.activity.Node(r.Context(), u.ID, node, activity.KindCreated, map[string]any{"team_folder": true})
	dto := nodeToDTO(node, nil).withCaps(share.Caps{View: true, Download: true, Share: true, Edit: true})
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
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
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}

	// A block document (M39) is an empty .md file; the office kinds start
	// from a template.
	markdown := body.Kind == "markdown"
	kind, ok := office.ParseKind(body.Kind)
	if !ok && !markdown {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "kind must be document|spreadsheet|presentation|markdown")
		return
	}
	ext := kind.Ext()
	if markdown {
		ext = ".md"
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		writeProblem(w, http.StatusBadRequest, "Bad Request", `name cannot contain / or \`)
		return
	}

	if !strings.EqualFold(path.Ext(name), ext) {
		name += ext
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

	var content []byte
	if !markdown {
		tpl, err := office.Template(kind, users.DecodePreferences(u.Settings).Locale)
		if err != nil {
			Error(w, r, err)
			return
		}
		content = tpl
	}

	node, ver, err := s.fs.PutFile(r.Context(), spaceID, parentID, pgtype.UUID{}, name, bytes.NewReader(content), u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}

	s.activity.Node(r.Context(), u.ID, node, activity.KindCreated, map[string]any{"template": body.Kind})
	writeJSON(w, http.StatusCreated, nodeToDTO(node, &ver).withCaps(share.CapsOwner))
}

// lockedByEditor answers status when a live lock exists, and reports whether
// the caller should stop. Both REST write paths that change which bytes are
// current have to ask; the WOPI paths already do, with their own header
// protocol (X-WOPI-Lock), which is why this is not shared with them.
//
// The status is the caller's: 409 on a plain REST route, 423 on the upload
// routes, where tus reserves 409 for "your offset is wrong" and a client that
// read a lock as that would HEAD and retry forever.
//
// allowRoom lets a block-document room's lock through: restoring a version
// goes through the room, which reseeds itself (collab.Service.Reseed). An
// upload replacing the file does not, and is refused like any other.
func (s *Server) lockedByEditor(w http.ResponseWriter, r *http.Request, nodeID pgtype.UUID, status int, allowRoom bool) bool {
	lockToken, err := s.wopi.CurrentLock(r.Context(), nodeID)
	if err != nil {
		Error(w, r, err)
		return true
	}
	if lockToken != "" && !(allowRoom && collab.IsLockToken(lockToken)) {
		writeProblem(w, status, http.StatusText(status),
			"this file is open in the editor; close it or wait for the lock to expire")
		return true
	}
	return false
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
	u, node, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if !node.ID.Valid {
		return
	}
	if node.Kind == store.NodeKindDir {
		s.audit.RecordRead(r, audit.Read{Kind: audit.KindFolderDownloaded, User: u, Node: node})
		s.streamZip(w, r, node)
		return
	}
	s.serveBlob(w, r, "attachment", share.CapDownload, audit.KindFileDownloaded)
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
	s.serveBlob(w, r, "inline", share.CapDownload, audit.KindFileViewed)
}

// serveBlob is the funnel for a file's bytes, and so where a read is audited:
// after authorisation and a successful open, before any byte is written.
func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, disposition string, need share.Cap, kind audit.Kind) {
	u, node, _ := s.requireNodeCaps(w, r, need)
	if !node.ID.Valid {
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	s.audit.RecordRead(r, audit.Read{Kind: kind, User: u, Node: n})

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
// there" before the move happens. Same preconditions as the move itself —
// write access at both ends and authorizeMove — so a move the server would
// refuse is refused here too, before the confirm dialog promises it.
//
// Moving a file out of somebody else's folder is a legitimate act — it is how
// a member takes back something they created and Drive documents it as the way
// to narrow sharing. What the endpoint adds is the consequence, in both
// directions: moving INTO a shared folder hands the file to everyone that
// folder reaches.
func (s *Server) handleMoveImpact(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapEdit)
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
	if !s.authorizeMove(w, r, u, node, caps, destID) {
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

// handleMoveNode relocates a node. Beyond write access at both ends, a move
// is bounded by authorizeMove, because in this model where a node hangs IS
// who can reach it.
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
	if !s.authorizeMove(w, r, u, node, caps, parentID) {
		return
	}
	if err := s.fs.Move(r.Context(), node.ID, parentID, u.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindMoved, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// authorizeMove applies the rules a move has beyond write access at both
// ends, writing the refusal itself. Access is inherited — ownership included
// — so a move re-decides who reaches the subtree, and three things must not
// ride on it:
//
//  1. Exporting team content. Taking content OUT of a team folder (to
//     personal space or another team folder) needs `CapShare` — a manager:
//     the subtree stops being the team's and becomes the mover's. Drive
//     requires Manager to move out of a shared drive for the same reason.
//  2. Donating other people's work to a team. Inside a team folder
//     `created_by` grants nothing, so moving a personal subtree in takes it
//     away from everyone who made part of it. Only someone who created the
//     node and everything live beneath it may do that — Drive's rule for
//     moving into a shared drive.
//  3. A non-owner re-deciding personal content. Ownership inherits down the
//     tree, so dropping someone else's file into a folder you own made you
//     its owner: an editor denied re-sharing came out with every capability,
//     public links included. A non-owner may not move a node anywhere that
//     would give them more than they hold on it now, and may not change who
//     reaches it at all without `CapShare` — Drive asks the same of an
//     editor moving out of a shared folder ("editors can change permissions
//     and share"). Owners move their own work freely: that is how a member
//     takes a file back out of a colleague's folder.
func (s *Server) authorizeMove(w http.ResponseWriter, r *http.Request, u *store.User, node store.Node, caps share.Caps, destID pgtype.UUID) bool {
	ctx := r.Context()
	var dest store.Node
	if destID.Valid {
		var err error
		if dest, err = s.fs.Get(ctx, destID); err != nil {
			s.nodeError(w, r, err)
			return false
		}
	}
	if node.TeamRootID.Valid {
		// Rule 1. Moving deeper inside the same team folder needs nothing more.
		if dest.TeamRootID != node.TeamRootID && !caps.Has(share.CapShare) {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"only a manager of this team folder can move content out of it")
			return false
		}
		return true
	}
	if dest.TeamRootID.Valid {
		// Rule 2.
		if node.CreatedBy != u.ID {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"only the person who created this can move it into a team folder — team content has no personal owner, so they would lose it")
			return false
		}
		foreign, err := s.fs.ForeignOwnedCount(ctx, node.ID, u.ID)
		if err != nil {
			Error(w, r, err)
			return false
		}
		if foreign > 0 {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"this folder holds items other people created; they would lose them inside a team folder, so move your own items instead")
			return false
		}
		return true
	}
	if caps.Owner {
		return true
	}
	// Rule 3.
	if destID.Valid {
		destCaps, err := s.share.EffectiveCapsForNode(ctx, dest, u.ID)
		if err != nil {
			Error(w, r, err)
			return false
		}
		if !caps.Contains(destCaps) {
			writeProblem(w, http.StatusForbidden, "Forbidden",
				"moving this there would give you more access to it than you have — make a copy instead")
			return false
		}
	}
	if caps.Has(share.CapShare) {
		return true
	}
	impact, err := s.fs.MoveImpact(ctx, node, destID)
	if err != nil {
		Error(w, r, err)
		return false
	}
	if len(principalDTOs(impact.Losing, u.ID))+len(principalDTOs(impact.Gaining, u.ID)) > 0 {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"this move changes who can reach the item; only its owner or someone allowed to share it can do that")
		return false
	}
	return true
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
	// A block document open in a room is restored through it: the room
	// writes back what it has not saved, then reseeds from the restored
	// version, so every peer sees it live. An office editor's lock still
	// refuses.
	if s.lockedByEditor(w, r, node.ID, http.StatusConflict, true) {
		return
	}
	restore := func(ctx context.Context) error { return s.fs.RestoreVersion(ctx, node.ID, versionID) }
	if err := s.collab.Reseed(r.Context(), node.ID, restore); err != nil {
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
			"this space mirrors no identity-provider organization yet; an instance administrator can link it")
	case errors.Is(err, orgsync.ErrAlreadyLinked), errors.Is(err, orgsync.ErrStrandedMembers):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	default:
		s.identityProviderError(w, r, err, "membership")
	}
}

// identityProviderError maps a failed write to Logto onto a status the admin
// can act on; what names the thing that could not be changed.
func (s *Server) identityProviderError(w http.ResponseWriter, r *http.Request, err error, what string) {
	switch {
	case errors.Is(err, orgsync.ErrNoIdentity):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable Entity",
			"this account was not issued by the identity provider, so "+what+" cannot be changed there")
	case errors.Is(err, orgsync.ErrUnreachable):
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable",
			"the identity provider is unreachable, so "+what+" cannot be changed right now")
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
