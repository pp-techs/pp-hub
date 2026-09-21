package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (s *Server) handleUpdateSpace(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Name               *string `json:"name"`
		Slug               *string `json:"slug"`
		Description        *string `json:"description"`
		DefaultAsAdmin     *bool   `json:"default_as_admin"`
		QuotaBytes         *int64  `json:"quota_bytes"`
		TrashRetentionDays *int32  `json:"trash_retention_days"`
		AllowPublicLinks   *bool   `json:"allow_public_links"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	next := store.UpdateSpaceSettingsParams{
		ID:                 space.ID,
		Name:               space.Name,
		Slug:               space.Slug,
		Description:        space.Description,
		DefaultAsAdmin:     space.DefaultAsAdmin,
		QuotaBytes:         space.QuotaBytes,
		TrashRetentionDays: space.TrashRetentionDays,
		AllowPublicLinks:   space.AllowPublicLinks,
	}
	if body.Name != nil {
		if strings.TrimSpace(*body.Name) == "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "name cannot be empty")
			return
		}
		next.Name = strings.TrimSpace(*body.Name)
	}
	if body.Slug != nil {
		slug := strings.TrimSpace(strings.ToLower(*body.Slug))
		if slug != "" && !slugPattern.MatchString(slug) {
			writeProblem(w, http.StatusBadRequest, "Bad Request",
				"slug may contain lowercase letters, numbers and dashes only")
			return
		}
		next.Slug = slug
	}
	if body.Description != nil {
		next.Description = strings.TrimSpace(*body.Description)
	}
	if body.DefaultAsAdmin != nil {
		next.DefaultAsAdmin = *body.DefaultAsAdmin
	}
	if body.QuotaBytes != nil {
		if *body.QuotaBytes < 0 {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "quota_bytes cannot be negative")
			return
		}
		next.QuotaBytes = *body.QuotaBytes
	}
	if body.TrashRetentionDays != nil {
		if *body.TrashRetentionDays < 0 {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "trash_retention_days cannot be negative")
			return
		}
		next.TrashRetentionDays = *body.TrashRetentionDays
	}
	if body.AllowPublicLinks != nil {
		next.AllowPublicLinks = *body.AllowPublicLinks
	}
	updated, err := s.store.UpdateSpaceSettings(r.Context(), next)
	if err != nil {
		if isUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "Conflict", "that slug is already taken")
			return
		}
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, spaceToDTO(updated, true, s.adminCount(r, updated.ID)))
	_ = u
}

func spaceSettingsFrom(sp store.Space, slug, description string, defaultAsAdmin bool, quota int64) (store.UpdateSpaceSettingsParams, string) {
	next := store.UpdateSpaceSettingsParams{
		ID:                 sp.ID,
		Name:               sp.Name,
		Slug:               sp.Slug,
		Description:        sp.Description,
		DefaultAsAdmin:     sp.DefaultAsAdmin,
		QuotaBytes:         sp.QuotaBytes,
		TrashRetentionDays: sp.TrashRetentionDays,
		AllowPublicLinks:   sp.AllowPublicLinks,
	}
	if slug != "" {
		normalised := strings.TrimSpace(strings.ToLower(slug))
		if !slugPattern.MatchString(normalised) {
			return next, "slug may contain lowercase letters, numbers and dashes only"
		}
		next.Slug = normalised
	}
	if description != "" {
		next.Description = strings.TrimSpace(description)
	}
	next.DefaultAsAdmin = defaultAsAdmin
	if quota < 0 {
		return next, "quota_bytes cannot be negative"
	}
	if quota > 0 {
		next.QuotaBytes = quota
	}
	return next, ""
}

func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	if err := s.store.DeleteSpace(r.Context(), spaceID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func uuidString(id pgtype.UUID) string {
	v, _ := id.Value()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func optionalTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(timeLayout)
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
