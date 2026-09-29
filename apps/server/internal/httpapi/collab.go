package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Block documents (M39): a markdown file edited in the block editor, live,
// through a collaboration room. See docs/architecture/block-documents.md.

type collabSessionDTO struct {
	// Room is what the client connects to and names its provider after:
	// `<nodeId>.<epoch>`. A different one than last time means the old
	// Y.Doc belongs to a previous generation and must be discarded.
	Room string `json:"room"`
	// CanWrite drives editor.setEditable. The server enforces it on its own
	// (a reader's writes are dropped), but a dropped edit would otherwise
	// stay in the reader's local copy.
	CanWrite bool `json:"can_write"`
	// Lossy: the file holds markdown the block editor cannot represent, and
	// saving from it rewrites the file.
	Lossy bool `json:"lossy"`
}

// handleCollabSession hands out a document's room. It takes download, as
// edit-session does for OnlyOffice and for the same reason: sync hands the
// whole document to the browser. The client asks again after every
// disconnect, so this is also where a revoked peer learns it is out.
func (s *Server) handleCollabSession(w http.ResponseWriter, r *http.Request) {
	u, node, caps := s.requireNodeCaps(w, r, share.CapDownload)
	if u == nil {
		return
	}
	if !s.isMarkdownFile(w, r, node) {
		return
	}
	sess, err := s.collab.Open(r.Context(), node)
	if err != nil {
		s.documentError(w, r, err)
		return
	}
	mode := "view"
	if caps.Edit {
		mode = "edit"
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileOpened, User: u, Node: node,
		Detail: map[string]any{"mode": mode, "editor": "blocks"}})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, collabSessionDTO{Room: sess.Room, CanWrite: caps.Edit, Lossy: sess.Lossy})
}

// isMarkdownFile answers 400 unless node is a file whose current version is a
// block document, and reports whether it is.
func (s *Server) isMarkdownFile(w http.ResponseWriter, r *http.Request, node store.Node) bool {
	if node.Kind == store.NodeKindFile {
		ver, err := s.store.GetCurrentVersion(r.Context(), node.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			Error(w, r, err)
			return false
		}
		if err == nil && fs.IsMarkdown(ver.Mime) {
			return true
		}
	}
	writeProblem(w, http.StatusBadRequest, "Bad Request", "not a markdown document")
	return false
}

// handleCollabSocket is a room's WebSocket. The session cookie authenticated
// it (the /api/v1 group) and socketOrigin checked its Origin; the room itself
// authorizes the document, the epoch and the mode (collab's Authorize).
func (s *Server) handleCollabSocket(w http.ResponseWriter, r *http.Request) {
	sess := callerSession(r)
	s.collab.ServeSocket(w, r, chi.URLParam(r, "room"), collab.Peer{User: sess.User.ID, Session: sess.ID})
}

// handleAttachment serves an image, video or file dropped into a document. It
// takes view on the document: the bytes are original content, and whoever can
// read the document sees its images. A hash the document does not hold is a
// 404 whether or not another document holds it.
func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil {
		return
	}
	s.serveAttachment(w, r, node, chi.URLParam(r, "hash"), audit.Read{Kind: audit.KindFileViewed, User: u, Node: node})
}
