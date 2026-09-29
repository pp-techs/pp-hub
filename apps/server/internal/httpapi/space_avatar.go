package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/avatar"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
)

// handleUploadSpaceAvatar stores a space's picture in the CAS and points the
// space at it. Admin-only: a space's identity is edited where its name is.
func (s *Server) handleUploadSpaceAvatar(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	if !parseUpload(w, r, avatar.MaxBytes) {
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "missing avatar file")
		return
	}
	defer file.Close()

	url, err := s.spaces.UploadAvatar(r.Context(), space.ID, file)
	switch {
	case errors.Is(err, avatar.ErrNotImage):
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", err.Error())
		return
	case errors.Is(err, avatar.ErrAvatarConflict):
		writeProblem(w, http.StatusConflict, "Conflict", "the avatar changed meanwhile; try again")
		return
	case err != nil:
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_url": url})
}

// handleClearSpaceAvatar removes a space's picture. Idempotent: a space that
// already has none is the state it is left in.
func (s *Server) handleClearSpaceAvatar(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	if err := s.spaces.ClearAvatar(r.Context(), space.ID); err != nil {
		if errors.Is(err, avatar.ErrAvatarConflict) {
			writeProblem(w, http.StatusConflict, "Conflict", "the avatar changed meanwhile; try again")
			return
		}
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetSpaceAvatar streams a space's picture.
//
// Members only, unlike the user avatar: a person's face is rendered by
// colleagues in other spaces (member lists, grantee pickers), but a space is
// a company and nothing outside it shows the picture. nosniff plus the
// stored, sniffed type is what keeps an uploaded file from being interpreted
// as anything else.
func (s *Server) handleGetSpaceAvatar(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.spaces.RequireMember(r.Context(), spaceID, u.ID); err != nil {
		s.spaceError(w, r, err)
		return
	}
	hash, mimeType, err := s.spaces.Avatar(r.Context(), spaceID)
	if err != nil && !errors.Is(err, spaces.ErrNotFound) {
		Error(w, r, err)
		return
	}
	if hash == "" {
		writeProblem(w, http.StatusNotFound, "Not Found", "no avatar")
		return
	}
	rc, err := s.blobs.Get(r.Context(), hash)
	if err != nil {
		if errors.Is(err, blob.ErrNotExist) {
			writeProblem(w, http.StatusNotFound, "Not Found", "no avatar")
			return
		}
		Error(w, r, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
}
