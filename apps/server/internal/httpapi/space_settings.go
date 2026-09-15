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
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// slugPattern matches what the settings form promises: lowercase letters,
// digits and dashes, not starting or ending with a dash.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const invitationTTL = 14 * 24 * time.Hour

type invitationDTO struct {
	ID        string `json:"id"`
	SpaceID   string `json:"space_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	ExpiresAt string `json:"expires_at,omitempty"`
	CreatedAt string `json:"created_at"`
	// URL is returned once, at creation. There is no mail transport in the
	// MVP, so the inviter copies this and sends it themselves.
	URL string `json:"url,omitempty"`
}

// handleUpdateSpace edits a space's own settings. Owner only.
func (s *Server) handleUpdateSpace(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceOwner(w, r)
	if !ok {
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Slug        *string `json:"slug"`
		Description *string `json:"description"`
		DefaultRole *string `json:"default_role"`
		QuotaBytes  *int64  `json:"quota_bytes"`
		// Days to keep trashed items. 0 keeps them indefinitely.
		TrashRetentionDays *int32 `json:"trash_retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid body")
		return
	}

	// Absent fields keep their current value, so the form can PATCH one field
	// without having to round-trip the rest.
	next := store.UpdateSpaceSettingsParams{
		ID:                 space.ID,
		Name:               space.Name,
		Slug:               space.Slug,
		Description:        space.Description,
		DefaultRole:        space.DefaultRole,
		QuotaBytes:         space.QuotaBytes,
		TrashRetentionDays: space.TrashRetentionDays,
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
	if body.DefaultRole != nil {
		role := store.MemberRole(*body.DefaultRole)
		switch role {
		case store.MemberRoleMember, store.MemberRoleAdmin:
			next.DefaultRole = role
		default:
			writeProblem(w, http.StatusBadRequest, "Bad Request", "default_role must be admin|member")
			return
		}
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
			writeProblem(w, http.StatusBadRequest, "Bad Request",
				"trash_retention_days cannot be negative")
			return
		}
		next.TrashRetentionDays = *body.TrashRetentionDays
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
	writeJSON(w, http.StatusOK, spaceToDTO(updated, string(spaces.RoleAdmin)))
	_ = u
}

// spaceSettingsFrom validates the optional settings supplied at creation and
// folds them onto the space's current values. Returns a problem detail string
// when something is invalid, so the caller can answer 400 without repeating
// the rules that handleUpdateSpace already enforces.
func spaceSettingsFrom(sp store.Space, slug, description, defaultRole string, quota int64) (store.UpdateSpaceSettingsParams, string) {
	next := store.UpdateSpaceSettingsParams{
		ID:                 sp.ID,
		Name:               sp.Name,
		Slug:               sp.Slug,
		Description:        sp.Description,
		DefaultRole:        sp.DefaultRole,
		QuotaBytes:         sp.QuotaBytes,
		TrashRetentionDays: sp.TrashRetentionDays,
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
	if defaultRole != "" {
		role := store.MemberRole(defaultRole)
		switch role {
		case store.MemberRoleMember, store.MemberRoleAdmin:
			next.DefaultRole = role
		default:
			return next, "default_role must be admin|member"
		}
	}
	if quota < 0 {
		return next, "quota_bytes cannot be negative"
	}
	if quota > 0 {
		next.QuotaBytes = quota
	}
	return next, ""
}

// handleDeleteSpace removes a space and everything in it. Owner only.
//
// The cascade is wide — nodes, versions, shares, members, activity — so the
// client is expected to confirm by name before calling this.
func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceOwner(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSpace(r.Context(), space.ID); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListInvitations returns the space's pending invitations.
func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceOwner(w, r)
	if !ok {
		return
	}
	rows, err := s.store.ListSpaceInvitations(r.Context(), space.ID)
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
			Role:      string(i.Role),
			InvitedBy: actorName(i.InviterDisplayName, i.InviterName, i.InviterEmail),
			ExpiresAt: optionalTime(i.ExpiresAt),
			CreatedAt: i.CreatedAt.Time.Format(timeLayout),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateInvitation invites an email address that may not have an account.
//
// If the address already belongs to a user, they are added as a member
// directly — an invitation would be a pointless extra step for someone who can
// already sign in.
func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	u, space, ok := s.requireSpaceOwner(w, r)
	if !ok {
		return
	}

	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
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
	role := store.MemberRole(body.Role)
	switch role {
	case store.MemberRoleMember, store.MemberRoleAdmin:
	case "":
		role = space.DefaultRole
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "role must be admin|member")
		return
	}

	if existing, err := s.store.GetUserByEmail(r.Context(), email); err == nil {
		if err := s.spaces.AddMember(r.Context(), space.ID, existing.ID, spaces.Role(role)); err != nil {
			Error(w, r, err)
			return
		}
		s.activity.Record(r.Context(), activity.Event{
			SpaceID: space.ID, ActorID: u.ID, Kind: activity.KindJoined,
			NodeName: existing.Email, Detail: map[string]any{"role": string(role)},
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
		Role:      role,
		TokenHash: hash,
		InvitedBy: u.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(invitationTTL), Valid: true},
	})
	if err != nil {
		Error(w, r, err)
		return
	}
	s.activity.Record(r.Context(), activity.Event{
		SpaceID: space.ID, ActorID: u.ID, Kind: activity.KindInvited,
		NodeName: email, Detail: map[string]any{"role": string(role)},
	})

	writeJSON(w, http.StatusCreated, invitationDTO{
		ID:        uuidString(inv.ID),
		SpaceID:   uuidString(inv.SpaceID),
		Email:     inv.Email,
		Role:      string(inv.Role),
		ExpiresAt: optionalTime(inv.ExpiresAt),
		CreatedAt: inv.CreatedAt.Time.Format(timeLayout),
		// Shown once. There is no mail transport yet, so the inviter sends it.
		URL: s.externalURL + "/invite/" + token,
	})
}

// handleRevokeInvitation withdraws a pending invitation.
func (s *Server) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	_, space, ok := s.requireSpaceOwner(w, r)
	if !ok {
		return
	}
	inviteID, err := uuidParam(r, "inviteID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid invitation id")
		return
	}
	if err := s.store.DeleteInvitation(r.Context(), store.DeleteInvitationParams{
		ID: inviteID, SpaceID: space.ID,
	}); err != nil {
		Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAcceptInvitation redeems a token for membership. Requires a signed-in
// user: the invitation names an address, but membership names an account.
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
	// The invitation is addressed to an email, not to whoever holds the link.
	if !strings.EqualFold(inv.Email, u.Email) {
		writeProblem(w, http.StatusForbidden, "Forbidden",
			"this invitation was sent to a different address")
		return
	}

	if err := s.spaces.AddMember(r.Context(), inv.SpaceID, u.ID, spaces.Role(inv.Role)); err != nil {
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
	writeJSON(w, http.StatusOK, spaceToDTO(space, string(inv.Role)))
}

// requireSpaceOwner resolves {spaceID} and demands the owner role.
func (s *Server) requireSpaceOwner(w http.ResponseWriter, r *http.Request) (*store.User, store.Space, bool) {
	u := s.requireUser(w, r)
	if u == nil {
		return nil, store.Space{}, false
	}
	spaceID, err := uuidParam(r, "spaceID")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid space id")
		return nil, store.Space{}, false
	}
	if err := s.spaces.RequireRole(r.Context(), spaceID, u.ID, spaces.RoleAdmin); err != nil {
		s.spaceError(w, r, err)
		return nil, store.Space{}, false
	}
	space, err := s.spaces.Get(r.Context(), spaceID)
	if err != nil {
		s.spaceError(w, r, err)
		return nil, store.Space{}, false
	}
	return u, space, true
}

// newInviteToken returns the plaintext token and the hash to store. Only the
// hash is persisted, so a database leak does not hand out memberships.
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

// isUniqueViolation mirrors fs.isUniqueViolation for the handlers that talk to
// the store directly.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
