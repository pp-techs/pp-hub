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
	id, _ := sp.ID.Value()
	owner, _ := sp.OwnerID.Value()
	return spaceDTO{
		ID:         id.(string),
		Name:       sp.Name,
		OwnerID:    owner.(string),
		UsedBytes:  sp.UsedBytes,
		QuotaBytes: sp.QuotaBytes,
		Role:       role,
		CreatedAt:  sp.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
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
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "name is required")
		return
	}
	sp, err := s.spaces.Create(r.Context(), body.Name, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, spaceToDTO(sp, string(spaces.RoleOwner)))
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

// requireNodeAccess resolves the node and enforces the effective permission:
// space role widened by direct shares on the node or its ancestors.
// minRank: 1=view, 2=download, 3=edit.
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
	// Space role rank: viewer=1, editor=2, owner=3. Non-members get 0.
	roleRank := 0
	if role, err := s.spaces.RoleOf(r.Context(), node.SpaceID, u.ID); err == nil {
		switch role {
		case spaces.RoleViewer:
			roleRank = 1
		case spaces.RoleEditor:
			roleRank = 2
		case spaces.RoleOwner:
			roleRank = 3
		}
	}
	eff, err := s.share.EffectivePerm(r.Context(), node, u.ID, roleRank)
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleViewer); err != nil {
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleEditor); err != nil {
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
	n, err := s.fs.Mkdir(r.Context(), spaceID, parentID, body.Name, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleEditor); err != nil {
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
	_, node := s.requireNodeAccess(w, r, 3)
	if !node.ID.Valid {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMoveNode(w http.ResponseWriter, r *http.Request) {
	_, node := s.requireNodeAccess(w, r, 3)
	if !node.ID.Valid {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	_, node := s.requireNodeAccess(w, r, 3)
	if !node.ID.Valid {
		return
	}
	if err := s.fs.Delete(r.Context(), node.ID); err != nil {
		s.nodeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	_, node := s.requireNodeAccess(w, r, 3)
	if !node.ID.Valid {
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleViewer); err != nil {
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
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleViewer); err != nil {
		s.spaceError(w, r, err)
		return
	}
	nodes, err := s.fs.ListDeleted(r.Context(), spaceID)
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
