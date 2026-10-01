package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// A document's text for agents (M41), mounted only on the agent dispatcher
// (apiRoutes with agent=true). An agent reads a file whole and writes a
// markdown document whole; the web client edits through the collaboration
// room instead, so these never reach the network.

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
	// Markdown marks a block document, which update_document can write;
	// Lossy one that holds constructs the editor cannot, which it cannot.
	Markdown bool   `json:"markdown"`
	Lossy    bool   `json:"lossy,omitempty"`
	Text     string `json:"text"`
}

// isAgentText reports whether a version can be handed to an agent as text.
func isAgentText(mimeType string) bool {
	return fs.IsMarkdown(mimeType) || strings.HasPrefix(strings.ToLower(mimeType), "text/")
}

// handleReadText is GET /nodes/{id}/text: a markdown or plain-text file as
// text. It takes view, like /content, and is audited as a view of the file.
// A block document open in the editor is flushed first, so the agent reads
// what the people in the room see, and the etag it gets is the one a write
// will be compared with.
func (s *Server) handleReadText(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	if node.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a folder has no text; list it instead")
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
	if !isAgentText(ver.Mime) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"only markdown and plain-text files can be read as text; send the person a link to open it in pp-hub")
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
	if !utf8.Valid(raw) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "this file is not UTF-8 text")
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

// errStaleText refuses a write whose base is no longer the current version.
var errStaleText = errors.New("the document changed since it was read")

// handleWriteMarkdown is PUT /nodes/{id}/text: {markdown, base_etag} becomes
// the document's next version, authored by the caller. It takes edit.
//
// The write goes through collab.Reseed, the path a version restore takes: a
// room open on the document writes its unsaved edits first, as a version of
// their own, and then holds the new content, so the people in it see the
// change at once. base_etag is compared inside that, after the flush — so a
// keystroke the agent never read makes the write stale rather than lost.
//
// Lossy markdown is refused: the document would open read-only in the web
// editor from then on, which is not an agent's call to make.
func (s *Server) handleWriteMarkdown(w http.ResponseWriter, r *http.Request) {
	if !limitBody(w, r, 2*maxAgentTextBytes) {
		return
	}
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil {
		return
	}
	var body struct {
		Markdown string `json:"markdown"`
		BaseETag string `json:"base_etag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	content := []byte(body.Markdown)
	if len(content) > maxAgentTextBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "the document is too large to write whole")
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
	if !fs.IsMarkdown(cur.Mime) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "only markdown documents can be written")
		return
	}
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
	// An office editor's lock still refuses; a room's is how the write is
	// carried to its peers.
	if s.lockedByEditor(w, r, node.ID, http.StatusConflict, true) {
		return
	}

	var written store.NodeVersion
	change := func(ctx context.Context) error {
		ver, err := s.store.GetCurrentVersion(ctx, node.ID)
		if err != nil {
			return err
		}
		if ver.BlobHash != body.BaseETag {
			return errStaleText
		}
		if blob.Hash(content) == ver.BlobHash {
			written = ver
			return nil
		}
		_, written, err = s.fs.PutFile(ctx, node.SpaceID, node.ParentID, node.ID, node.Name, bytes.NewReader(content), u.ID)
		return err
	}
	if err := s.collab.Reseed(r.Context(), node.ID, change); err != nil {
		if errors.Is(err, errStaleText) {
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
