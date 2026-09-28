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
	// The name is the organization's: rename it upstream first, or the next
	// reconcile would put the old one back.
	if next.Name != space.Name {
		if err := s.orgs.Rename(r.Context(), space, next.Name); err != nil {
			s.identityProviderError(w, r, err, "the space name")
			return
		}
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

// handleDeleteSpace deletes the space and everything in it. Its organization
// stays in Logto — deletion does not travel — so it is recorded as detached
// in the same transaction, or its members' next sign-in would recreate an
// empty space for it.
func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	err := s.store.InTx(r.Context(), func(q *store.Queries) error {
		if space.IdpOrgID.Valid {
			if err := q.DetachOrg(r.Context(), store.DetachOrgParams{
				OrgID: space.IdpOrgID.String, DetachedBy: u.ID,
			}); err != nil {
				return err
			}
		}
		return q.DeleteSpace(r.Context(), space.ID)
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type linkPreviewDTO struct {
	// Pushable members join the new organization with their current role.
	Pushable []spaceMemberRefDTO `json:"pushable"`
	// Stranded members have no account in the Logto tenant; linking turns
	// each of them into a departure.
	Stranded []spaceMemberRefDTO `json:"stranded"`
}

type spaceMemberRefDTO struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

func memberRefs(users []store.User) []spaceMemberRefDTO {
	out := make([]spaceMemberRefDTO, 0, len(users))
	for _, u := range users {
		out = append(out, spaceMemberRefDTO{UserID: uuidString(u.ID), Email: u.Email, Name: u.Name})
	}
	return out
}

// linkTarget resolves the space for the two link routes, which belong to an
// instance administrator: linking creates a company upstream.
func (s *Server) linkTarget(w http.ResponseWriter, r *http.Request) (store.Space, bool) {
	if s.requireAdmin(w, r) == nil {
		return store.Space{}, false
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return store.Space{}, false
	}
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return store.Space{}, false
	}
	return space, true
}

// handleLinkPreview says what linking an unlinked space would do to its
// members, before anybody commits to it.
func (s *Server) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	space, ok := s.linkTarget(w, r)
	if !ok {
		return
	}
	pushable, stranded, err := s.orgs.LinkPreview(r.Context(), space)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, linkPreviewDTO{Pushable: memberRefs(pushable), Stranded: memberRefs(stranded)})
}

// handleLinkSpace creates an organization for an unlinked space and queues
// the push of its members; the space reads as linked once that has run.
// Members with no Logto account must be acknowledged with confirm_stranded.
func (s *Server) handleLinkSpace(w http.ResponseWriter, r *http.Request) {
	space, ok := s.linkTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		ConfirmStranded bool `json:"confirm_stranded"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
			return
		}
	}
	orgID, err := s.orgs.Link(r.Context(), space, body.ConfirmStranded)
	if err != nil {
		s.spaceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "linking", "idp_org_id": orgID})
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
