package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Text access for agents, mounted only on the in-process agent dispatcher.
// Markdown uses the collaboration room; other supported UTF-8 files are
// replaced verbatim while holding the same editor lock as WOPI and CSV.

// maxAgentTextBytes bounds what an agent reads or writes in one go. Far below
// blockdoc.MaxBytes: past this a document no longer fits in a model's context
// anyway, and a truncated read written back would silently drop the rest.
const maxAgentTextBytes = 1 << 20

// agentTextDTO is a file's text and the etag a write must name.
type agentTextDTO struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Version int32  `json:"version"`
	// ETag is the current version's content hash. A write names it, and is
	// refused once anything else has written since.
	ETag string `json:"etag"`
	// Markdown marks a block document, which is written through collaboration;
	// Lossy marks constructs the block editor cannot preserve.
	Markdown bool   `json:"markdown"`
	Lossy    bool   `json:"lossy,omitempty"`
	Text     string `json:"text"`
}

// isAgentText recognizes supported text types, including extensions which the
// host MIME table may omit or misclassify (notably TypeScript as video/mp2t).
// The bytes must still pass validAgentText before they can be read or edited.
func isAgentText(name, mimeType string) bool {
	base, _, _ := strings.Cut(strings.ToLower(mimeType), ";")
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, "text/") {
		return true
	}
	if strings.HasPrefix(base, "application/") && (strings.HasSuffix(base, "+json") || strings.HasSuffix(base, "+xml")) {
		return true
	}
	switch base {
	case "application/json", "application/ld+json", "application/xml",
		"application/javascript", "application/x-javascript", "application/ecmascript",
		"application/typescript", "application/x-typescript", "application/yaml",
		"application/x-yaml", "image/svg+xml":
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	if base != "" && base != "application/octet-stream" && !(ext == ".ts" && base == "video/mp2t") {
		return false
	}
	switch ext {
	case ".md", ".markdown", ".txt", ".text", ".html", ".htm", ".css",
		".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx",
		".json", ".jsonc", ".xml", ".yaml", ".yml", ".svg", ".csv":
		return true
	default:
		return false
	}
}

func validAgentText(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	for _, b := range raw {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' {
			return false
		}
	}
	// Valid UTF-8 alone is insufficient: an ASCII-only PDF is still binary.
	return strings.HasPrefix(http.DetectContentType(raw), "text/")
}

// acquireAgentTextLock never steals a live editor's lock, and keeps our unique
// token until the operation has finished, including a conditional commit.
func (s *Server) acquireAgentTextLock(w http.ResponseWriter, r *http.Request, nodeID pgtype.UUID) (string, func(), bool) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		Error(w, r, err)
		return "", nil, false
	}
	token := "agent-text:" + hex.EncodeToString(random[:])
	n, err := s.store.AcquireLock(r.Context(), store.AcquireLockParams{NodeID: nodeID, LockToken: token})
	if err != nil {
		Error(w, r, err)
		return "", nil, false
	}
	if n == 0 {
		writeProblem(w, http.StatusConflict, "Conflict", "this file is open in the editor; close it or wait for the lock to expire")
		return "", nil, false
	}
	return token, func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		_ = s.store.DeleteLock(ctx, store.DeleteLockParams{NodeID: nodeID, LockToken: token})
	}, true
}

// handleReadText returns one whole UTF-8 file and the hash a write must name.
// A Markdown room is flushed first; other files refuse active editor locks so
// an agent cannot unknowingly read an older version than an editor is showing.
func (s *Server) handleReadText(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder has no text; list it instead")
		return
	}
	cur, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	markdown := fs.IsMarkdown(cur.Mime)
	if !markdown {
		_, release, ok := s.acquireAgentTextLock(w, r, node.ID)
		if !ok {
			return
		}
		defer release()
		u, node, _ = s.requireNodeCaps(w, r, share.CapView)
		if u == nil {
			return
		}
	} else if err := s.collab.Flush(r.Context(), node.ID); err != nil {
		Error(w, r, err)
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	if fs.IsMarkdown(ver.Mime) != markdown {
		writeProblem(w, http.StatusConflict, "Conflict", "the file changed type; read it again")
		return
	}
	if !isAgentText(n.Name, ver.Mime) || (r.URL.Query().Get("markdown") == "true" && !markdown) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"this is not a supported text file; use read_text_file for UTF-8 source files, read_office_file for Office documents, or download_file for binary files")
		return
	}
	if ver.Size > maxAgentTextBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this file is too large to read whole; open it in pp-hub")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxAgentTextBytes+1))
	if err != nil {
		Error(w, r, err)
		return
	}
	if len(raw) > maxAgentTextBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "this file is too large to read whole; open it in pp-hub")
		return
	}
	if !validAgentText(raw) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "this file is not UTF-8 text or contains binary control bytes")
		return
	}
	dto := agentTextDTO{
		NodeID: uuidString(n.ID), Name: n.Name, Mime: ver.Mime, Version: ver.VersionNo,
		ETag: ver.BlobHash, Markdown: fs.IsMarkdown(ver.Mime), Text: string(raw),
	}
	if dto.Markdown {
		parsed, err := mdoc.Parse(raw)
		if err != nil {
			Error(w, r, err)
			return
		}
		dto.Lossy = parsed.Lossy
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileViewed, User: u, Node: n})
	writeJSON(w, http.StatusOK, dto)
}

// maxAgentFileBytes bounds what download_file hands over in one answer. It is
// JSON with the bytes in base64, so the wire carries a third more.
const maxAgentFileBytes = 8 << 20

// agentFileDTO is a file's bytes, base64 on the wire.
type agentFileDTO struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
	Version int32  `json:"version"`
	ETag    string `json:"etag"`
	Content []byte `json:"content_base64"`
}

// handleReadFile is GET /nodes/{id}/file: any file's bytes, for an agent that
// can process them (an office document, a PDF, an image). It takes download,
// like /download — handing the bytes over deliberately is that right, not
// view's — and is audited as a download of the file. A block document open in
// the editor is flushed first, as handleReadText does.
func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder has no content; list it instead")
		return
	}
	if err := s.collab.Flush(r.Context(), node.ID); err != nil {
		Error(w, r, err)
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	if ver.Size > maxAgentFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this file is too large to download here; send the person a link to open it in pp-hub")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxAgentFileBytes+1))
	if err != nil {
		Error(w, r, err)
		return
	}
	if len(raw) > maxAgentFileBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"this file is too large to download here; send the person a link to open it in pp-hub")
		return
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileDownloaded, User: u, Node: n})
	writeJSON(w, http.StatusOK, agentFileDTO{
		NodeID: uuidString(n.ID), Name: n.Name, Mime: ver.Mime, Size: int64(len(raw)),
		Version: ver.VersionNo, ETag: ver.BlobHash, Content: raw,
	})
}

// handleWriteMarkdown retains the original {markdown, base_etag} document API
// and accepts {text, base_etag} for read_text_file/update_text_file. Exactly one
// content field is required, so a misspelled field cannot empty a document.
// Markdown still passes through mdoc and collab.Reseed; other files remain exact
// UTF-8 bytes and hold an editor lock from the read through the commit.
func (s *Server) handleWriteMarkdown(w http.ResponseWriter, r *http.Request) {
	// A UTF-8 byte may be JSON-escaped as six ASCII bytes.
	if !limitBody(w, r, 6*maxAgentTextBytes+1024) {
		return
	}
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	var body struct {
		Markdown *string `json:"markdown"`
		Text     *string `json:"text"`
		BaseETag string  `json:"base_etag"`
	}
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "the file is too large to write whole")
		} else {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		}
		return
	}
	if !utf8.Valid(rawBody) || json.Unmarshal(rawBody, &body) != nil || (body.Markdown == nil) == (body.Text == nil) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "provide exactly one of markdown or text")
		return
	}
	text := body.Text
	if body.Markdown != nil {
		text = body.Markdown
	}
	content := []byte(*text)
	if len(content) > maxAgentTextBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "the file is too large to write whole")
		return
	}
	if !validAgentText(content) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "content must be UTF-8 text without binary control bytes")
		return
	}
	if body.BaseETag == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "base_etag is required")
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder has no text")
		return
	}
	cur, err := s.store.GetCurrentVersion(r.Context(), node.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	markdown := fs.IsMarkdown(cur.Mime)
	if !isAgentText(node.Name, cur.Mime) || (body.Markdown != nil && !markdown) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "use text for supported UTF-8 files; markdown is only for Markdown documents")
		return
	}
	var token string
	if markdown {
		parsed, err := mdoc.Parse(content)
		if err != nil {
			Error(w, r, err)
			return
		}
		if parsed.Lossy {
			writeProblem(w, http.StatusUnprocessableEntity, "Unprocessable",
				"this markdown has constructs the editor cannot hold (raw HTML, footnotes, …); keep to headings, lists, tables, quotes, code, links and emphasis")
			return
		}
		if s.lockedByEditor(w, r, node.ID, http.StatusConflict, true) {
			return
		}
	} else {
		var release func()
		var ok bool
		token, release, ok = s.acquireAgentTextLock(w, r, node.ID)
		if !ok {
			return
		}
		defer release()
		// Check the stored bytes as well as the declared MIME/name: a binary
		// renamed to .txt is not a text file an agent may replace.
		currentNode, version, rc, err := s.fs.OpenFile(r.Context(), node.ID)
		if err != nil {
			s.nodeError(w, r, err)
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, maxAgentTextBytes+1))
		_ = rc.Close()
		if readErr != nil {
			Error(w, r, readErr)
			return
		}
		if version.Size > maxAgentTextBytes || len(raw) > maxAgentTextBytes {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "this file is too large to edit whole; open it in pp-hub")
			return
		}
		if !isAgentText(currentNode.Name, version.Mime) || fs.IsMarkdown(version.Mime) || !validAgentText(raw) {
			writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "this file is not supported UTF-8 text")
			return
		}
	}

	var written store.NodeVersion
	accessRefused := false
	change := func(ctx context.Context) error {
		latestUser, latestNode, _ := s.requireNodeCaps(w, r.WithContext(ctx), share.CapEdit)
		if latestUser == nil {
			accessRefused = true
			return errors.New("text write access was refused")
		}
		if latestNode.SpaceID != node.SpaceID || latestNode.ParentID != node.ParentID || latestNode.Name != node.Name {
			return fs.ErrStaleVersion
		}
		var err error
		_, written, err = s.fs.PutFileIfCurrent(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, bytes.NewReader(content), u.ID, body.BaseETag,
			s.agentCommitCheck(u.ID, node, token, markdown))
		return err
	}
	if markdown {
		err = s.collab.Reseed(r.Context(), node.ID, change)
	} else {
		err = change(r.Context())
	}
	if err != nil {
		if agentEditProblem(w, err) {
			return
		}
		if accessRefused {
			return
		}
		if errors.Is(err, fs.ErrStaleVersion) {
			writeProblem(w, http.StatusConflict, "Conflict",
				"the document changed since you read it; read it again and reapply your change")
			return
		}
		s.nodeError(w, r, err)
		return
	}
	if written.ID != cur.ID {
		s.activity.Node(r.Context(), u.ID, node, activity.KindUploaded, map[string]any{
			"size": written.Size, "version": written.VersionNo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": uuidString(node.ID), "version": written.VersionNo, "etag": written.BlobHash,
	})
}
