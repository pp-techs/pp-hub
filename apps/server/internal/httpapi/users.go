package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
)

// --- User Profile handlers ---

type userProfileDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Bio         string `json:"bio"`
	IsAdmin     bool   `json:"is_admin"`
	// CanCreateSpaces: whether "new space" is an action this account has.
	// False for an ordinary member once organizations own membership — a
	// space is a company, and companies come from the identity provider.
	// The client hides the control rather than letting it answer 403.
	CanCreateSpaces bool              `json:"can_create_spaces"`
	CreatedAt       string            `json:"created_at"`
	LastSeen        string            `json:"last_seen,omitempty"`
	Preferences     users.Preferences `json:"preferences"`
}

type updateProfileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
}

func profileToDTO(p users.Profile, canCreateSpaces bool) userProfileDTO {
	return userProfileDTO{
		ID:              p.ID,
		Email:           p.Email,
		Name:            p.Name,
		DisplayName:     p.DisplayName,
		AvatarURL:       p.AvatarURL,
		Bio:             p.Bio,
		IsAdmin:         p.IsAdmin,
		CanCreateSpaces: canCreateSpaces,
		CreatedAt:       p.CreatedAt,
		LastSeen:        p.LastSeen,
		Preferences:     p.Preferences,
	}
}

// handleUpdateMyPreferences applies a partial patch to the caller's
// account-level settings.
//
// PATCH with pointer fields, like handleUpdateSpace: a client that knows
// about only one preference must not blank the ones it has never heard of,
// and that property is what lets this document grow without a migration or a
// version field. It is deliberately not part of PUT /users/me, which is a
// full replace of display_name and bio.
func (s *Server) handleUpdateMyPreferences(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	var patch users.PreferencesPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	if patch.Locale != nil && !users.ValidLocale(strings.TrimSpace(*patch.Locale)) {
		writeProblem(w, http.StatusBadRequest, "Bad Request",
			"locale must be a language tag such as \"vi\" or \"vi-VN\"")
		return
	}
	prefs, err := s.users.UpdatePreferences(r.Context(), u.ID, patch)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// handleGetMyProfile returns the current user's full profile.
func (s *Server) handleGetMyProfile(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	profile, err := s.users.GetMe(r.Context(), u.ID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Error", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(profile, s.canCreateSpaces(u)))
}

// handleUpdateMyProfile updates the current user's profile.
func (s *Server) handleUpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "invalid body")
		return
	}
	profile, err := s.users.UpdateProfile(r.Context(), u.ID, req.DisplayName, req.Bio)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(profile, s.canCreateSpaces(u)))
}

// avatarMimes are the types an avatar may be. Sniffed from the bytes, never
// taken from the filename: the file is served back with this value, so a
// caller who could choose it could serve HTML from our origin.
var avatarMimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// handleUploadAvatar stores an avatar's bytes in the CAS and points the
// caller's profile at them.
func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if !parseUpload(w, r, avatarMaxBytes) {
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "missing avatar file")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		Error(w, r, err)
		return
	}
	head = head[:n]
	mimeType := http.DetectContentType(head)
	if !avatarMimes[mimeType] {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type",
			"an avatar must be a PNG, JPEG, GIF or WebP image")
		return
	}

	prev, _, err := s.users.Avatar(r.Context(), u.ID)
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		Error(w, r, err)
		return
	}

	hash, size, err := s.blobs.Put(r.Context(), io.MultiReader(bytes.NewReader(head), file))
	if err != nil {
		Error(w, r, fmt.Errorf("blob put: %w", err))
		return
	}
	// Mirrors fs.PutFile's Put → UpsertBlob → IncrBlobRef sequence — skipped
	// when re-uploading the identical image, so ref_count is not inflated and
	// gc.Worker.Sweep does not think the blob is briefly unreferenced.
	if hash != prev {
		if err := s.store.UpsertBlob(r.Context(), store.UpsertBlobParams{Hash: hash, Size: size}); err != nil {
			Error(w, r, err)
			return
		}
		if err := s.store.IncrBlobRef(r.Context(), hash); err != nil {
			Error(w, r, err)
			return
		}
	}

	// The query suffix exists so a new avatar is not masked by the browser's
	// cache of the old one.
	url := fmt.Sprintf("/api/v1/users/%s/avatar?v=%s", uuidString(u.ID), hash[:12])
	if err := s.users.SetAvatar(r.Context(), u.ID, url, hash, mimeType); err != nil {
		Error(w, r, err)
		return
	}
	if prev != "" && prev != hash {
		// The new avatar is already live; the direction of a ref-count drift
		// here is the safe one (too high — GC never over-collects), so this
		// is logged rather than failing the request.
		if err := s.store.DecrBlobRef(r.Context(), prev); err != nil {
			slog.ErrorContext(r.Context(), "avatar: decrementing previous blob ref failed", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_url": url})
}

// handleGetAvatar streams a user's avatar.
//
// Authenticated but not narrowed to shared spaces: member lists, group
// drawers and the people table already render every colleague's avatar_url,
// and the route is keyed on a UUID the caller must already hold. nosniff plus
// the stored, sniffed type is what keeps an uploaded file from being
// interpreted as anything else.
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	userID, err := uuidParam(r, "userID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user id")
		return
	}
	hash, mimeType, err := s.users.Avatar(r.Context(), userID)
	if err != nil && !errors.Is(err, users.ErrNotFound) {
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

// handleSearchUsers handles user search for autocomplete.
func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeProblem(w, http.StatusBadRequest, "Error", "missing query")
		return
	}
	limit := int32(20)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = int32(v)
		}
	}
	results, err := s.users.Search(r.Context(), "default", u.ID, u.IsAdmin, q, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "search failed")
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// handleListUsers returns all users (admin only).
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if !u.IsAdmin {
		writeProblem(w, http.StatusForbidden, "Error", "admin required")
		return
	}
	limit := int32(50)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = int32(v)
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = int32(v)
		}
	}
	// Read the user rows directly rather than through users.Profile: the
	// admin console needs the suspension flag, which a public-facing profile
	// deliberately does not carry.
	rows, err := s.store.ListUsers(r.Context(), store.ListUsersParams{
		TenantID: "default", Limit: limit, Offset: offset,
	})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "list failed")
		return
	}
	total, err := s.store.CountUsers(r.Context(), "default")
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "list failed")
		return
	}
	out := make([]adminUserDTO, 0, len(rows))
	for _, row := range rows {
		count, _ := s.store.CountUserSpaces(r.Context(), row.ID)
		out = append(out, adminUserToDTO(row, count))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users": out,
		"total": total,
	})
}
