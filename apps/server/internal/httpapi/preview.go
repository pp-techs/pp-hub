package httpapi

import (
	"io"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

// handlePreview serves a generated preview blob for a node's current version.
// ?kind=thumbnail|pdf|video|text — default thumbnail.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	_, node := s.requireNodeAccess(w, r, 1)
	if !node.ID.Valid {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "thumbnail"
	}
	var versionID pgtype.UUID
	if node.CurrentVersionID.Valid {
		versionID = node.CurrentVersionID
	} else {
		writeProblem(w, http.StatusNotFound, "Not Found", "no current version")
		return
	}
	rc, err := s.preview.GetPreviewBlob(r.Context(), versionID, kind)
	if err != nil {
		Error(w, r, err)
		return
	}
	if rc == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "preview not ready")
		return
	}
	defer rc.Close()
	switch kind {
	case "thumbnail":
		w.Header().Set("Content-Type", "image/png")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = io.Copy(w, rc)
}
