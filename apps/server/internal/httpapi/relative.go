package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Relative references (block-documents.md#relative-references): a block
// document's `![](images/a.png)` or `[spec](../spec.pdf)` names a file by its
// place beside the document — what an agent writes through a mount, what a
// repository's docs/ folder holds, what an export unpacks to. The markdown
// keeps the path; these routes resolve it against the document's place in
// the tree each time it is read, so the document shows the file as it is now.
//
// Who may have the bytes: whoever may read the file anyway (view for an image
// or video shown in place, download for anything else, which only
// downloads), or — the document's reach — a reader of the document when the
// file lies under the folder the document sits in and the author of the
// version being read may download it. The author condition is what keeps a
// person who was handed a single document from naming its neighbours to read
// them; it is the rule write-back adoption already follows for attachments.
// Anything refused is a 404, like a path that names nothing.

// relativeNodeDTO is what a relative link names, for the client to open it.
type relativeNodeDTO struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Markdown bool   `json:"markdown"`
}

// relativeEmbedDTO is a relative reference made one of the document's own
// attachments.
type relativeEmbedDTO struct {
	Src  string `json:"src"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// handleRelative serves the file a relative reference in a block document
// names: ?path= as written in the markdown, ?version= the version being read
// (the current one when absent), whose author decides the document's reach.
func (s *Server) handleRelative(w http.ResponseWriter, r *http.Request) {
	u, doc, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil || !s.isMarkdownFile(w, r, doc) {
		return
	}
	author, ok := s.versionAuthor(w, r, doc, r.URL.Query().Get("version"))
	if !ok {
		return
	}
	target, ok := s.relativeTarget(w, r, doc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	own, err := s.share.EffectiveCapsForNode(r.Context(), target, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	s.serveRelative(w, r, doc, target, own, author,
		audit.Read{Kind: audit.KindFileViewed, User: u, Node: target})
}

// handleShareRelative is handleRelative through a link: the document must be
// inside the link, and the link's own terms stand for the visitor's access to
// the file — a file the link also covers is the visitor's to view.
func (s *Server) handleShareRelative(w http.ResponseWriter, r *http.Request) {
	sh, root, doc, ok := s.shareScopeRoot(w, r, "node")
	if !ok || !s.isMarkdownFile(w, r, doc) {
		return
	}
	author, ok := s.versionAuthor(w, r, doc, "")
	if !ok {
		return
	}
	target, ok := s.relativeTarget(w, r, doc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	var own share.Caps
	if _, err := s.resolveShareNode(r, root, target.ID); err == nil {
		if own, err = share.LinkCaps(sh); err != nil {
			Error(w, r, err)
			return
		}
	}
	s.serveRelative(w, r, doc, target, own, author,
		audit.Read{Kind: audit.KindFileViewed, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: target})
}

// handleRelativeMeta answers what a relative link in a block document names,
// for the client to open it. Opening is the reader's own business, so only a
// file or folder they can view themselves is answered.
func (s *Server) handleRelativeMeta(w http.ResponseWriter, r *http.Request) {
	u, doc, _ := s.requireNodeCaps(w, r, share.CapView)
	if u == nil || !s.isMarkdownFile(w, r, doc) {
		return
	}
	target, ok := s.relativeTarget(w, r, doc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	own, err := s.share.EffectiveCapsForNode(r.Context(), target, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !own.View {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return
	}
	s.writeRelativeNode(w, r, target)
}

// handleShareRelativeMeta is handleRelativeMeta through a link: only a node
// the link covers is answered, so the visitor can open it in the link.
func (s *Server) handleShareRelativeMeta(w http.ResponseWriter, r *http.Request) {
	_, root, doc, ok := s.shareScopeRoot(w, r, "node")
	if !ok || !s.isMarkdownFile(w, r, doc) {
		return
	}
	target, ok := s.relativeTarget(w, r, doc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	if _, err := s.resolveShareNode(r, root, target.ID); err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "not found in this share")
		return
	}
	s.writeRelativeNode(w, r, target)
}

// handleRelativeEmbed makes the file a relative reference names one of the
// document's attachments, so the document keeps it whatever happens to the
// file. The client then points the block at the answered src. A copy of the
// file is what this makes, so it takes the caller's own download on the file
// — never the document's reach — and edit on the document.
func (s *Server) handleRelativeEmbed(w http.ResponseWriter, r *http.Request) {
	u, doc, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil || !s.isMarkdownFile(w, r, doc) {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	target, ok := s.relativeTarget(w, r, doc, body.Path)
	if !ok {
		return
	}
	if target.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a file")
		return
	}
	own, err := s.share.EffectiveCapsForNode(r.Context(), target, u.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !own.View {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return
	}
	if !own.Download {
		writeProblem(w, http.StatusForbidden, "Forbidden", "you cannot download this file, so it cannot be copied into the document")
		return
	}
	att, err := s.fs.AttachFile(r.Context(), doc.ID, target, u.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, relativeEmbedDTO{
		Src: mdoc.AttachmentScheme + att.BlobHash, Name: att.Name, Mime: att.Mime, Size: att.Size,
	})
}

// relativeTarget resolves ref against doc, writing the response on failure:
// 400 for something that is not a relative path, 404 for one naming nothing.
func (s *Server) relativeTarget(w http.ResponseWriter, r *http.Request, doc store.Node, ref string) (store.Node, bool) {
	segs, ok := mdoc.RelativePath(ref)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a relative path")
		return store.Node{}, false
	}
	target, err := s.fs.ResolveRelative(r.Context(), doc, segs)
	if errors.Is(err, fs.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return store.Node{}, false
	}
	if err != nil {
		Error(w, r, err)
		return store.Node{}, false
	}
	return target, true
}

// versionAuthor is who wrote the version of doc being read: versionID when
// given (one of doc's own), else the current version.
func (s *Server) versionAuthor(w http.ResponseWriter, r *http.Request, doc store.Node, versionID string) (pgtype.UUID, bool) {
	var (
		ver store.NodeVersion
		err error
	)
	if versionID != "" {
		var id pgtype.UUID
		if err := id.Scan(versionID); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid version id")
			return pgtype.UUID{}, false
		}
		ver, err = s.store.GetVersion(r.Context(), id)
		if err == nil && ver.NodeID != doc.ID {
			err = pgx.ErrNoRows
		}
	} else {
		ver, err = s.store.GetCurrentVersion(r.Context(), doc.ID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeProblem(w, http.StatusNotFound, "Not Found", "version not found")
		return pgtype.UUID{}, false
	}
	if err != nil {
		Error(w, r, err)
		return pgtype.UUID{}, false
	}
	return ver.CreatedBy, true
}

// relativeMedia reports whether a file of type mime is shown in place in a
// document — a raster image or a video. Everything else, SVG included,
// downloads.
func relativeMedia(mimeType string) bool {
	t := strings.ToLower(mimeType)
	return (strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/")) && inlineDisposition(mimeType) == "inline"
}

// relativeReadable decides whether a reader holding own on target may have
// its bytes through doc, whose version being read is author's (see the
// package comment above).
func (s *Server) relativeReadable(ctx context.Context, doc, target store.Node, media bool, own share.Caps, author pgtype.UUID) (bool, error) {
	if own.Download || (media && own.View) {
		return true, nil
	}
	if !author.Valid {
		return false, nil
	}
	inside, err := s.fs.InDocumentFolder(ctx, doc, target)
	if err != nil || !inside {
		return false, err
	}
	caps, err := s.share.EffectiveCapsForNode(ctx, target, author)
	if err != nil {
		return false, err
	}
	return caps.Download, nil
}

// serveRelative writes target's current bytes when relativeReadable allows,
// recording rd (its detail gains the document). Images and videos render in
// place; anything else downloads. The file can change under the same path,
// so the response is revalidated by its blob hash rather than cached.
func (s *Server) serveRelative(w http.ResponseWriter, r *http.Request, doc, target store.Node, own share.Caps, author pgtype.UUID, rd audit.Read) {
	if target.Kind != store.NodeKindFile {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return
	}
	ver, err := s.store.GetCurrentVersion(r.Context(), target.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	media := relativeMedia(ver.Mime)
	ok, err := s.relativeReadable(r.Context(), doc, target, media, own, author)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such file")
		return
	}
	n, ver, rc, err := s.fs.OpenFile(r.Context(), target.ID)
	if err != nil {
		s.nodeError(w, r, err)
		return
	}
	defer rc.Close()
	rd.Detail = map[string]any{"document": doc.ID.String()}
	s.audit.RecordRead(r, rd)

	disposition := "attachment"
	if relativeMedia(ver.Mime) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", ver.Mime)
	w.Header().Set("Content-Disposition", contentDisposition(disposition, n.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"`+ver.BlobHash+`"`)
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		rs = &forwardSeeker{r: rc, size: ver.Size}
	}
	http.ServeContent(w, r, n.Name, ver.CreatedAt.Time, rs)
}

// writeRelativeNode answers what a relative link names.
func (s *Server) writeRelativeNode(w http.ResponseWriter, r *http.Request, target store.Node) {
	out := relativeNodeDTO{ID: target.ID.String(), Kind: string(target.Kind), Name: target.Name}
	if target.Kind == store.NodeKindFile {
		ver, err := s.store.GetCurrentVersion(r.Context(), target.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			Error(w, r, err)
			return
		}
		out.Markdown = err == nil && fs.IsMarkdown(ver.Mime)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
