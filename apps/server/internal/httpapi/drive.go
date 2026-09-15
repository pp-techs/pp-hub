package httpapi

import (
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
	Role       string `json:"role,omitempty"`
	CreatedAt  string `json:"created_at"`
	// Settings fields. These must be emitted even when empty: the settings
	// form reads the current values from here and PATCHes the whole set back,
	// so omitting an empty slug would make the form clear it on the next save.
	Slug        string `json:"slug"`
	Description string `json:"description"`
	DefaultRole string `json:"default_role"`
	// Days to keep trashed items. 0 keeps them indefinitely.
	TrashRetentionDays int32 `json:"trash_retention_days"`
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
	ModifiedAt string `json:"modified_at,omitempty"` // latest version.CreatedAt (file) or node.CreatedAt (folder)
	Owner      string `json:"owner,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
	CreatedAt  string `json:"created_at"`
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

func spaceToDTO(sp store.Space, role string) spaceDTO {
	// uuidString rather than a bare type assertion: Value() returns nil for an
	// invalid UUID, and nil.(string) panics. Reachable through any path that
	// hands back a zero-value Space on an error it did not check.
	return spaceDTO{
		ID:                 uuidString(sp.ID),
		Name:               sp.Name,
		OwnerID:            uuidString(sp.OwnerID),
		UsedBytes:          sp.UsedBytes,
		QuotaBytes:         sp.QuotaBytes,
		Role:               role,
		CreatedAt:          sp.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		Slug:               sp.Slug,
		Description:        sp.Description,
		DefaultRole:        string(sp.DefaultRole),
		TrashRetentionDays: sp.TrashRetentionDays,
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

// nodeToDTOMeta builds a nodeDTO from a NodeWithMeta row (includes modified_at + owner).
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
	// Populate version fields from the joined latest version row.
	if n.VerID.Valid {
		dto.Size = n.VerSize
		dto.Mime = n.VerMime
		dto.Version = n.VerVersionNo
		dto.ModifiedAt = n.VerCreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	// For dirs with no version, modified_at = node.CreatedAt.
	if n.Kind == store.NodeKindDir && dto.ModifiedAt == "" {
		dto.ModifiedAt = n.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	// Owner = node creator's display name (from LEFT JOIN users).
	if n.OwnerName != "" {
		dto.Owner = n.OwnerName
	} else if n.OwnerEmail != "" {
		dto.Owner = n.OwnerEmail
	}
	// OwnerID: use node.created_by (always set).
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
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		DefaultRole string `json:"default_role"`
		QuotaBytes  int64  `json:"quota_bytes"`
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

	// Creation takes the same optional settings the settings screen edits, but
	// the space row has to exist before they can be applied — a slug collision
	// should reject the settings, not lose the space. So they land as a second
	// write, and a failure leaves a usable space with default settings.
	if body.Slug != "" || body.Description != "" || body.DefaultRole != "" || body.QuotaBytes > 0 {
		settings, problem := spaceSettingsFrom(sp, body.Slug, body.Description, body.DefaultRole, body.QuotaBytes)
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
	writeJSON(w, http.StatusCreated, spaceToDTO(sp, string(spaces.RoleAdmin)))
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
		role, _ := s.spaces.RoleOf(r.Context(), sp.ID, u.ID)
		out = append(out, spaceToDTO(sp, string(role)))
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Node handlers ---

// spaceRoleRank maps a space role onto the *permission* scale
// (view=1, download=2, edit=3) — not onto the role scale, which is a separate
// comparison used only by spaces.RequireRole.
//
// admin → 3: an admin holds edit everywhere in the space they administer.
// member → 0: belonging to the company grants no access to any file. A
// member's permission comes entirely from group grants and direct shares, so
// this returns the floor and share.EffectivePerm supplies the rest.
//
// This is the single seam where "is a member" could be turned back into
// implicit access. It is deliberately the only copy: the WOPI handler used to
// keep its own switch on raw role strings, which silently drifted and minted
// read-only tokens for users who could write.
func (s *Server) spaceRoleRank(r *http.Request, spaceID, userID pgtype.UUID) int {
	// A non-member resolves to 0 and falls through to the share lookup, which
	// is correct: a direct share can name someone outside the space.
	role, err := s.spaces.RoleOf(r.Context(), spaceID, userID)
	if err != nil {
		return 0
	}
	return roleFloor(role)
}

// roleFloor is the pure half of spaceRoleRank, split out so the mapping can be
// tested without a database.
func roleFloor(role spaces.Role) int {
	if role == spaces.RoleAdmin {
		return 3
	}
	return 0
}

// requireParentWrite authorises creating something inside parentID.
//
// The space-keyed create routes (POST /spaces/{spaceID}/dirs and .../files)
// take parent_id from the request body, and until this existed nothing checked
// it: not that it belonged to spaceID, not that the caller could write there.
// That was harmless only while an editor could write anywhere in their space.
// Now that a member's access comes from individual grants, an unchecked
// parent_id would let anyone holding one grant write into any folder of any
// company.
//
// A parent in another space answers 404, not 403 — confirming that a node
// exists is already more than the caller should learn, the same rule anonymous
// share browsing follows.
//
// A nil parent means the space root, which nobody holds a grant on, so it is
// admin-only.
func (s *Server) requireParentWrite(w http.ResponseWriter, r *http.Request, spaceID, parentID pgtype.UUID, u *store.User) bool {
	if !parentID.Valid {
		if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
			writeProblem(w, http.StatusForbidden, "Forbidden", "only an admin can create items at the top level of a space")
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
	eff, err := s.share.EffectivePerm(r.Context(), parent, u.ID, s.spaceRoleRank(r, spaceID, u.ID))
	if err != nil {
		Error(w, r, err)
		return false
	}
	if eff < 3 {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return false
	}
	return true
}

// requireNodeAccess resolves the node and enforces the effective permission:
// the space-role floor widened by group grants and direct shares on the node
// or its ancestors. minRank: 1=view, 2=download, 3=edit.
func (s *Server) requireNodeAccess(w http.ResponseWriter, r *http.Request, minRank int) (*store.User, store.Node) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, store.Node{}
	}
	nodeID, err := uuidParam(r, "nodeID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid node id")
		return nil, store.Node{}
	}
	node, err := s.fs.Get(r.Context(), nodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return nil, store.Node{}
	}
	eff, err := s.share.EffectivePerm(r.Context(), node, u.ID, s.spaceRoleRank(r, node.SpaceID, u.ID))
	if err != nil {
		Error(w, r, err)
		return nil, store.Node{}
	}
	if eff < minRank {
		writeProblem(w, http.StatusForbidden, "Forbidden", "insufficient permission")
		return nil, store.Node{}
	}
	return u, node
}

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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
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
	children, err := s.fs.ListWithMeta(r.Context(), spaceID, parentID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(children))
	for _, n := range children {
		out = append(out, nodeToDTOMeta(n))
	}
	writeJSON(w, http.StatusOK, out)
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
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
	writeJSON(w, http.StatusCreated, nodeToDTO(n, nil))
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
		s.spaceError(w, r, err)
		return
	}
	// multipart: fields parent_id (optional), file
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
	s.recount(r, spaceID)
	writeJSON(w, http.StatusCreated, nodeToDTO(node, &ver))
}

// inlineMimes are the types /content may hand the browser to render in place.
// Blobs are served from the app's own origin, so anything that can execute
// there (HTML, SVG, scripts) is deliberately absent and falls back to a
// download. Mirrored by INLINE_MIMES in apps/web/src/lib/api/utils.ts.
var inlineMimes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"video/quicktime": true,
	"audio/mpeg":      true,
	"audio/mp4":       true,
	"audio/ogg":       true,
	"audio/wav":       true,
	"text/plain":      true,
}

// inlineDisposition reports how mimeType may be served: "inline" for the
// allowlisted types, "attachment" for everything else.
func inlineDisposition(mimeType string) string {
	// Strip parameters ("text/plain; charset=utf-8") before matching.
	base, _, _ := strings.Cut(mimeType, ";")
	if inlineMimes[strings.TrimSpace(strings.ToLower(base))] {
		return "inline"
	}
	return "attachment"
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// A folder has no blob of its own; downloading one means its contents,
	// which is a streamed zip. Callers hit the same URL either way.
	_, node := s.requireNodeAccess(w, r, 2)
	if !node.ID.Valid {
		return
	}
	if node.Kind == store.NodeKindDir {
		s.streamZip(w, r, node)
		return
	}
	s.serveBlob(w, r, "attachment")
}

// handleContent streams the file's own bytes for in-browser viewing: the PDF
// viewer, <img>, <video>. Distinct from /preview, which serves the small blob
// the worker generated, and from /download, which forces a save dialog.
func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, "inline")
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, disposition string) {
	_, node := s.requireNodeAccess(w, r, 1)
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
	if cd == "" { // unencodable filename — keep the disposition, drop the name
		cd = disposition
	}
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", cd)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// ServeContent answers Range requests, which PDF viewers and <video> use to
	// seek. The local blob store hands back an *os.File, so the assertion holds.
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, n.Name, ver.CreatedAt.Time, rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(ver.Size, 10))
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleRenameNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3)
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
	u, node := s.requireNodeAccess(w, r, 3)
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
	if err := s.fs.Move(r.Context(), node.ID, parentID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindMoved, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCopyNode duplicates a node, optionally into another folder.
func (s *Server) handleCopyNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3)
	if u == nil {
		return
	}
	var body struct {
		ParentID string `json:"parent_id"` // empty = alongside the original
	}
	// An empty body is valid: copy in place.
	_ = json.NewDecoder(r.Body).Decode(&body)

	dest := node.ParentID
	if body.ParentID != "" {
		if err := dest.Scan(body.ParentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid parent_id")
			return
		}
	}

	copied, err := s.fs.Copy(r.Context(), node.ID, dest, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, copied, activity.KindCreated,
		map[string]any{"copied_from": node.Name})
	s.recount(r, node.SpaceID)
	writeJSON(w, http.StatusCreated, nodeToDTO(copied, nil))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	u, node := s.requireNodeAccess(w, r, 3)
	if u == nil {
		return
	}
	if err := s.fs.Delete(r.Context(), node.ID, u.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	s.activity.Node(r.Context(), u.ID, node, activity.KindTrashed, nil)
	s.recount(r, node.SpaceID)
	w.WriteHeader(http.StatusNoContent)
}

// handleGetNode returns one node with its current version and owner.
//
// Added for the full-page editor, which is routed by node id alone and
// otherwise has no way to learn the file's own name. The breadcrumb trail in
// the drive header works around the same absence by recording folders as the
// user walks them; that workaround can now be retired.
func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	_, node := s.requireNodeAccess(w, r, 1)
	if !node.ID.Valid {
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
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	_, node := s.requireNodeAccess(w, r, 1)
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
	u, node := s.requireNodeAccess(w, r, 3)
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
	s.recount(r, node.SpaceID)
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
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
	nodes, err := s.fs.List(r.Context(), spaceID, parentID)
	if err != nil {
		Error(w, r, err)
		return
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
		out = append(out, treeNodeDTO{
			ID:          func() string { id, _ := n.ID.Value(); return id.(string) }(),
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleMember); err != nil {
		s.spaceError(w, r, err)
		return
	}
	rows, err := s.fs.ListDeletedWithMeta(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]trashNodeDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, trashRowToDTO(n))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListShared(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	nodes, err := s.fs.ListShared(r.Context(), u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeToDTO(n, nil))
	}
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
	default:
		Error(w, r, err)
	}
}
