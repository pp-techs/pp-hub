package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	CreatedAt   string `json:"created_at"`
	LastSeen    string `json:"last_seen,omitempty"`
}

type updateProfileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
}

func profileToDTO(p users.Profile) userProfileDTO {
	return userProfileDTO{
		ID:          p.ID,
		Email:       p.Email,
		Name:        p.Name,
		DisplayName: p.DisplayName,
		AvatarURL:   p.AvatarURL,
		Bio:         p.Bio,
		IsAdmin:     p.IsAdmin,
		CreatedAt:   p.CreatedAt,
		LastSeen:    p.LastSeen,
	}
}

// handleGetMyProfile returns the current user's full profile.
func (s *Server) handleGetMyProfile(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	profile, err := s.users.GetMe(r.Context(), u.ID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Error", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(profile))
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
	writeJSON(w, http.StatusOK, profileToDTO(profile))
}

// handleUploadAvatar handles avatar upload (multipart → blob).
func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u := caller(r)
	if err := r.ParseMultipartForm(5 << 20); err != nil { // 5MB max
		writeProblem(w, http.StatusRequestEntityTooLarge, "Error", "file too large")
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Error", "missing avatar file")
		return
	}
	defer file.Close()

	// TODO: store to blob store and get URL
	// For now, use a placeholder - in real impl, stream to blob.Put
	avatarURL := "/api/v1/users/me/avatar" // placeholder

	if err := s.users.SetAvatar(r.Context(), u.ID, avatarURL); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Error", "avatar update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_url": avatarURL})
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
