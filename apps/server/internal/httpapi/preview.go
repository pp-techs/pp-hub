package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
)

// previewCacheControl caches for a year, and privately.
//
// Caching this hard is safe because the client asks at a version-keyed URL and
// preview blobs are content-addressed: editing a file changes the URL rather
// than the bytes behind it, so a cached copy can never go stale.
//
// "private" is load-bearing rather than cautious: this response is only
// reached through requireNodeCaps, so a shared proxy that cached it would
// serve one company's thumbnail to a member of another — straight past the
// folder grants in docs/architecture/permissions.md.
const previewCacheControl = "private, max-age=31536000, immutable"

// previewETag wraps a blob hash as an entity tag. Previews are
// content-addressed, so the hash is an exact validator at no extra cost.
func previewETag(hash string) string { return `"` + hash + `"` }

// etagMatches implements the If-None-Match comparison we need. A client may
// send a list, and may weaken a tag with the W/ prefix on revalidation.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if strings.EqualFold(strings.TrimPrefix(candidate, "W/"), etag) {
			return true
		}
	}
	return false
}

// handlePreview serves a generated preview blob for a node's current version.
// ?kind=thumbnail|text — default thumbnail.
//
// The two kinds share one bar now: both describe the file for a reader, and
// view is what a reader holds. The text snippet is a verbatim 4 KiB prefix, so
// it is the file's own bytes in miniature — the same position /content takes,
// where the render and the save are the same stream.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = preview.PreviewThumbnail
	}
	_, node, _ := s.requireNodeCaps(w, r, share.CapView)
	if !node.ID.Valid {
		return
	}
	if kind != preview.PreviewThumbnail && kind != preview.PreviewText {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "unknown preview kind")
		return
	}
	var versionID pgtype.UUID
	if node.CurrentVersionID.Valid {
		versionID = node.CurrentVersionID
	} else {
		writeProblem(w, http.StatusNotFound, "Not Found", "no current version")
		return
	}
	rc, hash, err := s.preview.GetPreviewBlob(r.Context(), versionID, kind)
	if err != nil {
		Error(w, r, err)
		return
	}
	if rc == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "preview not ready")
		return
	}
	defer rc.Close()

	etag := previewETag(hash)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", previewCacheControl)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if kind == preview.PreviewThumbnail {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
}
