package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/blockdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/unfurl"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// Block documents outside the editor: a version as a document tree for a
// read-only view (signed in or through a link), the export with attachments,
// the templates a new one starts from, and link bookmarks. See
// docs/architecture/block-documents.md.

// documentDTO is a version of a block document as the editor's schema sees
// it, for a read-only view to render.
type documentDTO struct {
	Doc     mdoc.Node `json:"doc"`
	Lossy   bool      `json:"lossy"`
	Version int32     `json:"version"`
}

// handleDocument answers a version of a block document — the current one, or
// ?version=<id> — as a document tree. The tree is the whole document, so it
// takes download, like /content and /collab.
func (s *Server) handleDocument(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if u == nil {
		return
	}
	var versionID pgtype.UUID
	if v := r.URL.Query().Get("version"); v != "" {
		if err := versionID.Scan(v); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid version id")
			return
		}
	}
	doc, err := blockdoc.New(s.fs).Document(r.Context(), node.ID, versionID)
	if err != nil {
		s.documentError(w, r, err)
		return
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileViewed, User: u, Node: node,
		Detail: map[string]any{"format": "document"}})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, documentDTO{Doc: doc.Doc, Lossy: doc.Lossy, Version: doc.Version})
}

// handleShareDocument is handleDocument through a link: the link must allow
// downloads and cover the node (?node=, the link's own node when absent).
func (s *Server) handleShareDocument(w http.ResponseWriter, r *http.Request) {
	sh, node, ok := s.shareScope(w, r, "node")
	if !ok || !linkAllowsDownload(w, sh) {
		return
	}
	doc, err := blockdoc.New(s.fs).Document(r.Context(), node.ID, pgtype.UUID{})
	if err != nil {
		s.documentError(w, r, err)
		return
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileViewed, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: node,
		Detail: map[string]any{"format": "document"}})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, documentDTO{Doc: doc.Doc, Lossy: doc.Lossy, Version: doc.Version})
}

// handleShareAttachment serves an attachment of a document inside a link, on
// the same terms as the document itself: the link allows downloads and covers
// the document.
func (s *Server) handleShareAttachment(w http.ResponseWriter, r *http.Request) {
	sh, node, ok := s.shareScope(w, r, "node")
	if !ok || !linkAllowsDownload(w, sh) {
		return
	}
	s.serveAttachment(w, r, node, chi.URLParam(r, "hash"),
		audit.Read{Kind: audit.KindFileViewed, ShareID: sh.ID, SharedBy: sh.CreatedBy, Node: node})
}

// documentError maps blockdoc's refusals; the rest are fs's.
func (s *Server) documentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, blockdoc.ErrNotMarkdown):
		writeProblem(w, http.StatusBadRequest, "Bad Request", "not a markdown document")
	case errors.Is(err, blockdoc.ErrTooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", err.Error())
	default:
		s.nodeError(w, r, err)
	}
}

// handleExport downloads a block document as a zip: its markdown, with every
// attachment it references pointing into assets/, and those attachments — a
// document that still renders once it leaves pp-hub. A download, audited as
// one.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapDownload)
	if u == nil {
		return
	}
	exp, err := blockdoc.New(s.fs).PrepareExport(r.Context(), node)
	if err != nil {
		s.documentError(w, r, err)
		return
	}
	s.audit.RecordRead(r, audit.Read{Kind: audit.KindFileDownloaded, User: u, Node: node,
		Detail: map[string]any{"export": "zip"}})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", exp.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if err := exp.WriteZip(r.Context(), w); err != nil {
		// Committed already: the archive ends where it failed.
		slog.WarnContext(r.Context(), "document export incomplete", "node", node.ID, "err", err)
	}
}

// handleListMarkdownTemplates is the catalog a new block document can start
// from, in ?locale= when given, else the caller's saved language.
func (s *Server) handleListMarkdownTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, blockdoc.Templates(s.templateLocale(r, r.URL.Query().Get("locale"))))
}

// templateLocale is the language a template is listed or written in: the one
// the client names, else the caller's saved preference.
func (s *Server) templateLocale(r *http.Request, requested string) string {
	if requested != "" {
		return requested
	}
	return users.DecodePreferences(caller(r).Settings).Locale
}

type unfurlDTO struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Thumbnail is the page's preview image, stored as an attachment of the
	// document: CSP keeps img-src to this origin, and a hotlinked image
	// would tell the site every time somebody read the document.
	Thumbnail string `json:"thumbnail,omitempty"`
}

// publicUnfurl fetches for a Server that was not given a fetcher: public
// addresses only, the production policy.
var publicUnfurl = unfurl.New(unfurl.Config{})

// handleUnfurl fetches a link for a bookmark block in a document the caller
// can edit. The fetch is the server's, so it is SSRF-safe by construction
// (internal/unfurl); the preview image is stored as the document's
// attachment, under the upload cap and the space's quota. A preview image
// that cannot be had leaves the bookmark without one rather than failing it.
func (s *Server) handleUnfurl(w http.ResponseWriter, r *http.Request) {
	u, node, _ := s.requireNodeCaps(w, r, share.CapEdit)
	if u == nil || !s.isMarkdownFile(w, r, node) {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	fetch := s.unfurl
	if fetch == nil {
		fetch = publicUnfurl
	}
	ctx := r.Context()
	page, err := fetch.Page(ctx, body.URL)
	switch {
	case errors.Is(err, unfurl.ErrInvalidURL), errors.Is(err, unfurl.ErrForbiddenAddress):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	case err != nil:
		slog.InfoContext(ctx, "unfurl failed", "node", node.ID, "err", err)
		writeProblem(w, http.StatusBadGateway, "Bad Gateway", "the link could not be fetched")
		return
	}
	out := unfurlDTO{URL: page.URL, Title: page.Title, Description: page.Description}
	if page.Image != "" {
		out.Thumbnail = s.storeThumbnail(r, fetch, node, page)
	}
	writeJSON(w, http.StatusOK, out)
}

// storeThumbnail fetches page's preview image into node's attachments and
// answers its src, or "" when there is none to be had.
func (s *Server) storeThumbnail(r *http.Request, fetch *unfurl.Fetcher, node store.Node, page unfurl.Page) string {
	ctx := r.Context()
	img, err := fetch.Image(ctx, page.Image, s.uploadCap())
	if err != nil {
		slog.InfoContext(ctx, "unfurl preview image skipped", "node", node.ID, "err", err)
		return ""
	}
	att, err := s.fs.PutAttachment(ctx, node.ID, thumbnailName(page.URL, img.Ext), bytes.NewReader(img.Body), caller(r).ID)
	if err != nil {
		if !errors.Is(err, fs.ErrQuotaExceeded) {
			slog.WarnContext(ctx, "store unfurl preview image", "node", node.ID, "err", err)
		}
		return ""
	}
	return mdoc.AttachmentScheme + att.BlobHash
}

// thumbnailName names a stored preview image after the site it came from, so
// an export's assets/ says where each one is from.
func thumbnailName(pageURL, ext string) string {
	host := "link"
	if u, err := url.Parse(pageURL); err == nil && u.Hostname() != "" {
		host = strings.TrimPrefix(u.Hostname(), "www.")
	}
	return host + "-preview" + ext
}

// serveAttachment writes one of doc's attachments, recording rd as the read
// (its detail gains the hash). Images and videos render in place — never SVG,
// which inlineMimes leaves out — and anything else downloads. Ranges are
// served, which a <video> needs to seek, whether or not the blob store can
// seek.
func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, doc store.Node, hash string, rd audit.Read) {
	if !blob.IsHash(hash) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such attachment")
		return
	}
	att, rc, err := s.fs.OpenAttachment(r.Context(), doc.ID, hash)
	if errors.Is(err, fs.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found", "no such attachment")
		return
	}
	if err != nil {
		Error(w, r, err)
		return
	}
	defer rc.Close()
	rd.Detail = map[string]any{"attachment": att.BlobHash}
	s.audit.RecordRead(r, rd)

	disposition := "attachment"
	if t := strings.ToLower(att.Mime); strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/") {
		disposition = inlineDisposition(att.Mime)
	}
	w.Header().Set("Content-Type", att.Mime)
	w.Header().Set("Content-Disposition", contentDisposition(disposition, att.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Content-addressed within the document: these bytes never change.
	// Private, because only this caller's access let them out.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+att.BlobHash+`"`)
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		rs = &forwardSeeker{r: rc, size: att.Size}
	}
	http.ServeContent(w, r, att.Name, att.CreatedAt.Time, rs)
}

// forwardSeeker lets http.ServeContent answer a Range request from a reader
// that cannot seek (an S3 object body): it reports the known size, and a seek
// forward discards up to the new offset on the next read. Seeking back past
// what was read fails, which only a multi-range request in descending order
// would need.
type forwardSeeker struct {
	r    io.Reader
	size int64
	read int64 // bytes consumed from r
	pos  int64 // logical offset
}

func (f *forwardSeeker) Read(p []byte) (int, error) {
	if f.pos > f.read {
		n, err := io.CopyN(io.Discard, f.r, f.pos-f.read)
		f.read += n
		if err != nil {
			return 0, err
		}
	}
	n, err := f.r.Read(p)
	f.read += int64(n)
	f.pos = f.read
	return n, err
}

func (f *forwardSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.pos + offset
	case io.SeekEnd:
		abs = f.size + offset
	default:
		return 0, errors.New("forwardSeeker: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("forwardSeeker: negative position")
	}
	// ServeContent measures with SeekEnd and returns to the start before
	// reading anything: a position is only a promise until Read.
	if abs < f.read {
		return 0, errors.New("forwardSeeker: cannot seek backwards")
	}
	f.pos = abs
	return abs, nil
}
