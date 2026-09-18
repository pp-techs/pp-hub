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
}

type nodeDTO struct {
	ID         string      `json:"id"`
	SpaceID    string      `json:"space_id"`
	ParentID   string      `json:"parent_id,omitempty"`
	Kind       string      `json:"kind"`
	Name       string      `json:"name"`
	Size       int64       `json:"size,omitempty"`
	Mime       string      `json:"mime,omitempty"`
	Version    int32       `json:"version,omitempty"`
	ModifiedAt string      `json:"modified_at,omitempty"`
	Owner      string      `json:"owner,omitempty"`
	OwnerID    string      `json:"owner_id,omitempty"`
	CreatedAt  string      `json:"created_at"`
	// Caps is the caller's resolved capability set on this node — what the
	// node menu enables. Set on listings and single-node GETs.
	Caps *share.Caps `json:"caps,omitempty"`
	// Shared reports whether this node itself carries a live share row —
	// direct, group or link, any grantee. It is not access: a node the
	// caller can only reach through an ancestor's grant is not itself
	// shared, and a node shared with someone else stays true even when the
	// caller holds no grant on it at all. Set alongside Caps.
	Shared bool `json:"shared,omitempty"`
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

func spaceToDTO(sp store.Space, isAdmin bool) spaceDTO {
	return spaceDTO{
		ID:                 uuidString(sp.ID),
		Name:               sp.Name,
		OwnerID:            uuidString(sp.OwnerID),
		UsedBytes:          sp.UsedBytes,
		QuotaBytes:         sp.QuotaBytes,
		IsAdmin:            isAdmin,
		CreatedAt:         sp.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		Slug:               sp.Slug,
		Description:        sp.Description,
		DefaultAsAdmin:     sp.DefaultAsAdmin,
		TrashRetentionDays: sp.TrashRetentionDays,
		ArchiveRootID:      uuidString(sp.ArchiveRootID),
	}
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
	return dto
}

// --- Spaces handlers ---

func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var body struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		Description   string `json:"description"`
		DefaultAsAdmin bool   `json:"default_as_admin"`
		QuotaBytes    int64  `json:"quota_bytes"`
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
	writeJSON(w, http.StatusCreated, spaceToDTO(sp, true)) // creator is admin of the space they create
}

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	list, err := s.spaces.ListForUser(r.Context(), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]spaceDTO, 0, len(list))
	for _, sp := range list {
		out = append(out, spaceToDTO(sp, s.spaces.IsAdmin(r.Context(), sp.ID, u.ID)))
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
	u := s.requireUser(w, r)
	if u == nil {
		return nil, store.Node{}, share.Caps{}
	}
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
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid multipart form")
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
	s.serveBlob(w, r, "attachment")
}

func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, "inline")
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, disposition string) {
	_, node, _ := s.requireNodeCaps(w, r, share.CapView)
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

// requireMovableNode authorises moving, renaming or trashing a node. The archive
// root itself cannot be moved, renamed or trashed.
func (s *Server) requireMovableNode(w http.ResponseWriter, r *http.Request) (*store.User, store.Node) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if !node.ID.Valid {
		return nil, store.Node{}
	}
	if s.isArchiveRoot(r.Context(), node) {
		writeProblem(w, http.StatusForbidden, "Forbidden", "the archive has a fixed place and cannot be moved, renamed or trashed")
		return nil, store.Node{}
	}
	return u, node
}

func (s *Server) handleRenameNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireMovableNode(w, r)
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

func (s *Server) handleMoveNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireMovableNode(w, r)
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
	if err := s.fs.Move(r.Context(), node.ID, parentID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindMoved, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCopyNode(w http.ResponseWriter, r *http.Request) {
	u, _, _ := s.requireNodeCaps(w, r, share.CapEdit)
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
	u, node := s.requireMovableNode(w, r)
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
	u, node := s.requireMovableNode(w, r)
	if u == nil {
		return
	}
	var versionID pgtype.UUID
	if err := versionID.Scan(chi.URLParam(r, "versionID")); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid version id")
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
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	rows, err := s.fs.ListDeletedWithMeta(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	// Filter by reachability: the ancestor walk does not filter deleted_at, so
	// a deleted file inside a deleted folder still resolves against the grant
	// above it.
	rows, err = reachableRows(r.Context(), s, u.ID, rows,
		func(n store.ListDeletedWithMetaRow) pgtype.UUID { return n.ID },
		func(n store.ListDeletedWithMetaRow) pgtype.UUID { return n.SpaceID })
	if err != nil {
		return
	}
	out := make([]trashNodeDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, trashNodeDTO{
			ID:         uuidString(n.ID),
			SpaceID:    uuidString(n.SpaceID),
			Kind:       string(n.Kind),
			Name:       n.Name,
			ModifiedAt: n.VCreatedAt.Time.Format(timeLayout),
			DeletedAt:  n.DeletedAt.Time.Format(timeLayout),
			PurgesAt:   n.PurgesAt.Time.Format(timeLayout),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListShared answers GET /api/v1/shared[?space=]. The entry-point query
// already carries an optional space filter, and it applies a LIMIT, so the
// scoping has to happen in SQL rather than on the assembled slice.
func (s *Server) handleListShared(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
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
	case errors.Is(err, fs.ErrCycle):
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
	case errors.Is(err, spaces.ErrLastAdmin):
		writeProblem(w, http.StatusConflict, "Conflict", err.Error())
	default:
		Error(w, r, err)
	}
}
