package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/activity"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const invitationTTL = 14 * 24 * time.Hour

type invitationDTO struct {
	ID        string `json:"id"`
	SpaceID   string `json:"space_id"`
	Email     string `json:"email"`
	AsAdmin   bool   `json:"as_admin"`
	InvitedBy string `json:"invited_by"`
	ExpiresAt string `json:"expires_at,omitempty"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url,omitempty"`
}

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
	writeJSON(w, http.StatusOK, spaceToDTO(updated, true))
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

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	rows, err := s.store.ListSpaceInvitations(r.Context(), spaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	out := make([]invitationDTO, 0, len(rows))
	for _, i := range rows {
		out = append(out, invitationDTO{
			ID:        uuidString(i.ID),
			SpaceID:   uuidString(i.SpaceID),
			Email:     i.Email,
			AsAdmin:   i.AsAdmin,
			InvitedBy: actorName(i.InviterDisplayName, i.InviterName, i.InviterEmail),
			ExpiresAt: optionalTime(i.ExpiresAt),
			CreatedAt: i.CreatedAt.Time.Format(timeLayout),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Email   string `json:"email"`
		AsAdmin bool   `json:"as_admin"`
		Role    string `json:"role"` // deprecated: "admin"|"member" → as_admin
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "a valid email is required")
		return
	}

	asAdmin := body.AsAdmin
	if body.Role == "admin" {
		asAdmin = true
	} else if body.Role == "member" {
		asAdmin = false
	} else if body.Role != "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "role must be admin|member")
		return
	}
	if !body.AsAdmin && body.Role == "" {
		asAdmin = space.DefaultAsAdmin
	}

	if existing, err := s.store.GetUserByEmail(r.Context(), email); err == nil {
		if err := s.spaces.AddMember(r.Context(), space.ID, existing.ID, asAdmin); err != nil {
			Error(w, r, err)
			return
		}
		roleStr := "member"
		if asAdmin {
			roleStr = "admin"
		}
		s.activity.Record(r.Context(), activity.Event{
			SpaceID: space.ID, ActorID: u.ID, Kind: activity.KindJoined,
			NodeName: existing.Email, Detail: map[string]any{"role": roleStr},
		})
		writeJSON(w, http.StatusOK, map[string]any{"added_member": true, "email": existing.Email})
		return
	}

	token, hash, err := newInviteToken()
	if err != nil {
		Error(w, r, err)
		return
	}
	inv, err := s.store.CreateInvitation(r.Context(), store.CreateInvitationParams{
		SpaceID:   space.ID,
		Email:     email,
		AsAdmin:   asAdmin,
		TokenHash: hash,
		InvitedBy: u.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(invitationTTL), Valid: true},
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	roleStr := "member"
	if asAdmin {
		roleStr = "admin"
	}
	s.activity.Record(r.Context(), activity.Event{
		SpaceID: space.ID, ActorID: u.ID, Kind: activity.KindInvited,
		NodeName: email, Detail: map[string]any{"role": roleStr},
	})
	writeJSON(w, http.StatusCreated, invitationDTO{
		ID:        uuidString(inv.ID),
		SpaceID:   uuidString(inv.SpaceID),
		Email:     inv.Email,
		AsAdmin:   inv.AsAdmin,
		ExpiresAt: optionalTime(inv.ExpiresAt),
		CreatedAt: inv.CreatedAt.Time.Format(timeLayout),
		URL:       s.externalURL + "/invite/" + token,
	})
}

func (s *Server) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.requireSpaceAdmin(w, r)
	if !ok {
		return
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return
	}
	inviteID, err := uuidParam(r, "inviteID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid invitation id")
		return
	}
	if err := s.store.DeleteInvitation(r.Context(), store.DeleteInvitationParams{
		ID: inviteID, SpaceID: spaceID,
	}); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "token is required")
		return
	}
	inv, err := s.store.GetInvitationByToken(r.Context(), hashToken(body.Token))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "invalid or expired invitation")
		return
	}
	if !strings.EqualFold(inv.Email, u.Email) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"this invitation was sent to a different address")
		return
	}
	if err := s.spaces.AddMember(r.Context(), inv.SpaceID, u.ID, inv.AsAdmin); err != nil {
		Error(w, r, err)
		return
	}
	if err := s.store.AcceptInvitation(r.Context(), inv.ID); err != nil {
		Error(w, r, err)
		return
	}
	s.activity.Record(r.Context(), activity.Event{
		SpaceID: inv.SpaceID, ActorID: u.ID, Kind: activity.KindJoined, NodeName: u.Email,
	})
	space, err := s.spaces.Get(r.Context(), inv.SpaceID)
	if err != nil {
		Error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, spaceToDTO(space, inv.AsAdmin))
}

func newInviteToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
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
