package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/officeedit"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// These routes exist only on the authenticated in-process agent dispatcher.
// The API owns capabilities, locks and versions; only the isolated runner
// parses Office bytes. Structured reads take view, not download.
type agentOfficeDTO struct {
	NodeID  string          `json:"node_id"`
	Name    string          `json:"name"`
	Mime    string          `json:"mime"`
	Format  string          `json:"format"`
	Version int32           `json:"version"`
	ETag    string          `json:"etag"`
	Data    json.RawMessage `json:"data"`
}

func agentOfficeFormat(name, mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return ""
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	var want string
	switch format {
	case "docx":
		want = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		want = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "pptx":
		want = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	if want == "" || mediaType != want {
		return ""
	}
	return format
}

func (s *Server) handleOfficeSchema(w http.ResponseWriter, r *http.Request) {
	data, err := officeedit.Schema(r.URL.Query().Get("format"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "format must be docx, xlsx or pptx")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleReadOffice(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder is not an Office file")
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	depth := 2
	if raw := r.URL.Query().Get("depth"); raw != "" {
		var err error
		depth, err = strconv.Atoi(raw)
		if err != nil || depth < 0 || depth > officeedit.MaxDepth {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "depth must be between 0 and 8")
			return
		}
	}
	if len(path) > officeedit.MaxPathBytes || !strings.HasPrefix(path, "/") {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "path must be an Office element path starting with /, at most 1024 bytes")
		return
	}
	// OnlyOffice's unsaved buffer is not in CAS. Never silently present an
	// older saved version as the live document.
	if s.lockedByEditor(w, r, node.ID, http.StatusConflict, false) {
		return
	}
	if s.officeEditor == nil {
		s.officeUnavailable(w)
		return
	}
	n, ver, content, ok := s.readOfficeContent(w, r, node.ID)
	if !ok {
		return
	}
	format := agentOfficeFormat(n.Name, ver.Mime)
	if format == "" {
		s.officeUnsupported(w)
		return
	}
	result, err := s.officeEditor.Do(r.Context(), officeedit.Request{
		Action: "read", Format: format, Content: content, Path: path, Depth: depth,
	})
	if err != nil {
		s.officeError(w, r, err)
		return
	}
	if !s.officeStillCurrent(w, r, node, ver, share.CapView) || s.lockedByEditor(w, r, node.ID, http.StatusConflict, false) {
		return
	}
	if !json.Valid(result.Data) {
		writeProblem(w, http.StatusBadGateway, "Bad Gateway", "the Office runner returned an invalid document response")
		return
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileViewed, User: u, Node: n})
	writeJSON(w, http.StatusOK, agentOfficeDTO{
		NodeID: uuidString(n.ID), Name: n.Name, Mime: ver.Mime, Format: format,
		Version: ver.VersionNo, ETag: ver.BlobHash, Data: result.Data,
	})
}

func (s *Server) handleEditOffice(w http.ResponseWriter, r *http.Request) {
	// The runner bounds the canonical marshaled operations JSON, not the
	// incoming spelling. A JSON character can arrive as a six-byte escape;
	// bound that wire form here, then apply the same canonical limit below.
	if !limitBody(w, r, 6*officeedit.MaxOperationsBytes+4096) {
		return
	}
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder is not an Office file")
		return
	}
	var body struct {
		BaseETag   string                 `json:"base_etag"`
		Operations []officeedit.Operation `json:"operations"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid Office edit body")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid Office edit body")
		return
	}
	if body.BaseETag == "" || len(body.BaseETag) > 256 || len(body.Operations) == 0 || len(body.Operations) > officeedit.MaxOperations {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "base_etag and between 1 and 100 operations are required")
		return
	}
	encoded, err := json.Marshal(body.Operations)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid Office operations")
		return
	}
	if len(encoded) > officeedit.MaxOperationsBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "Office operations must be at most 1 MiB as JSON")
		return
	}
	if s.officeEditor == nil {
		s.officeUnavailable(w)
		return
	}
	// A fresh UUID per call is essential: AcquireLock may refresh its own
	// token, so a user- or node-stable token would admit concurrent edits.
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		Error(w, r, err)
		return
	}
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	token := "agent-office:" + uuidString(pgtype.UUID{Bytes: random, Valid: true})
	acquired, err := s.store.AcquireLock(r.Context(), store.AcquireLockParams{NodeID: node.ID, LockToken: token})
	if err != nil {
		Error(w, r, err)
		return
	}
	if acquired != 1 {
		writeProblem(w, http.StatusConflict, "Conflict", "this file is open in an editor or another edit; close it or wait for the lock to expire")
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		if err := s.store.DeleteLock(ctx, store.DeleteLockParams{NodeID: node.ID, LockToken: token}); err != nil {
			slog.WarnContext(ctx, "release Office agent lock", "node", node.ID, "err", err)
		}
	}()
	n, ver, content, ok := s.readOfficeContent(w, r, node.ID)
	if !ok {
		return
	}
	if ver.BlobHash != body.BaseETag {
		s.officeStale(w)
		return
	}
	if !s.officeStillCurrent(w, r, node, ver, share.CapEdit) {
		return
	}
	format := agentOfficeFormat(n.Name, ver.Mime)
	if format == "" {
		s.officeUnsupported(w)
		return
	}
	result, err := s.officeEditor.Do(r.Context(), officeedit.Request{
		Action: "edit", Format: format, Content: content, Operations: body.Operations,
	})
	if err != nil {
		s.officeError(w, r, err)
		return
	}
	if len(result.Content) == 0 || len(result.Content) > officeedit.MaxFileBytes {
		writeProblem(w, http.StatusBadGateway, "Bad Gateway", "the Office runner returned an invalid file size")
		return
	}
	if cap := s.uploadCap(); cap > 0 && int64(len(result.Content)) > cap {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "the edited file exceeds this instance's upload limit")
		return
	}
	// Processing is outside a transaction. Re-resolve grants and live
	// ancestry now: moves, trash and revocations during processing must not
	// authorize a write with the old snapshot's access.
	if !s.officeStillCurrent(w, r, node, ver, share.CapEdit) {
		return
	}
	lock, err := s.wopi.CurrentLock(r.Context(), node.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if lock != token {
		writeProblem(w, http.StatusConflict, "Conflict", "the Office edit lock changed; read the file again")
		return
	}
	// FS atomically rechecks the node metadata and content hash under its
	// row lock before committing the new version.
	_, written, err := s.fs.PutFileIfCurrent(r.Context(), node.SpaceID, node.ParentID, node.ID, node.Name,
		bytes.NewReader(result.Content), u.ID, body.BaseETag, s.agentCommitCheck(u.ID, node, token, false))
	if agentEditProblem(w, err) {
		return
	}
	if errors.Is(err, fs.ErrStaleVersion) {
		s.officeStale(w)
		return
	}
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	if written.ID != ver.ID {
		s.activity.Node(r.Context(), u.ID, node, activity.KindUploaded, map[string]any{
			"size": written.Size, "version": written.VersionNo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": uuidString(node.ID), "version": written.VersionNo, "etag": written.BlobHash,
	})
}

func (s *Server) readOfficeContent(w http.ResponseWriter, r *http.Request, nodeID pgtype.UUID) (store.Node, store.NodeVersion, []byte, bool) {
	n, ver, rc, err := s.fs.OpenFile(r.Context(), nodeID)
	if err != nil {
		s.nodeError(w, r, err)
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	defer rc.Close()
	if !s.officeLiveAncestry(w, r, n) {
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	if agentOfficeFormat(n.Name, ver.Mime) == "" {
		s.officeUnsupported(w)
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	if ver.Size > officeedit.MaxFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "Office agent files must be at most 8 MiB")
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	content, err := io.ReadAll(io.LimitReader(rc, officeedit.MaxFileBytes+1))
	if err != nil {
		Error(w, r, err)
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	if len(content) > officeedit.MaxFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "Office agent files must be at most 8 MiB")
		return store.Node{}, store.NodeVersion{}, nil, false
	}
	return n, ver, content, true
}

func (s *Server) officeStillCurrent(w http.ResponseWriter, r *http.Request, initial store.Node, ver store.NodeVersion, need share.Cap) bool {
	u, current, _ := s.requireNodeCapsByID(w, r, initial.ID, need)
	if u == nil {
		return false
	}
	if current.SpaceID != initial.SpaceID || current.ParentID != initial.ParentID || current.TeamRootID != initial.TeamRootID || current.Name != initial.Name || current.CurrentVersionID != ver.ID {
		s.officeStale(w)
		return false
	}
	return s.officeLiveAncestry(w, r, current)
}

// Capability resolution intentionally includes trash so the trash UI can
// restore it. Office processing is only for the live tree, including parents.
func (s *Server) officeLiveAncestry(w http.ResponseWriter, r *http.Request, node store.Node) bool {
	if node.DeletedAt.Valid {
		s.nodeError(w, r, fs.ErrNotFound)
		return false
	}
	parent := node.ParentID
	for depth := 0; parent.Valid && depth < 100; depth++ {
		n, err := s.fs.Get(r.Context(), parent)
		if err != nil {
			s.nodeError(w, r, err)
			return false
		}
		parent = n.ParentID
	}
	if parent.Valid {
		writeProblem(w, http.StatusConflict, "Conflict", "the file's folder tree is too deep to process")
		return false
	}
	return true
}

func (s *Server) officeUnsupported(w http.ResponseWriter) {
	writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "only DOCX, XLSX and PPTX files with their matching Office media type are supported")
}

func (s *Server) officeStale(w http.ResponseWriter) {
	writeProblem(w, http.StatusConflict, "Conflict", "the file changed or moved since you read it; read it again and reapply your operations")
}

func (s *Server) officeUnavailable(w http.ResponseWriter) {
	writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable", "Office agent processing is not configured or is unavailable")
}

func (s *Server) officeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, officeedit.ErrUnavailable):
		s.officeUnavailable(w)
	case errors.Is(err, officeedit.ErrInvalidRequest):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", "invalid Office operations or file; use get_office_schema and read_office_file to choose supported paths and properties")
	case errors.Is(err, officeedit.ErrProcessing):
		writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable", "the Office operations could not be applied; no changes were saved")
	default:
		Error(w, r, err)
	}
}
