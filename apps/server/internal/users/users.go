// Package users implements user profile management.
// User records are created on first OIDC login via UpsertUser.
package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var ErrNotFound = errors.New("user not found")

// Profile is the public user profile.
type Profile struct {
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

// SearchResult is a minimal user representation for autocomplete.
type SearchResult struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

// Service wraps user queries with profile logic.
type Service struct {
	q *store.Queries
}

func NewService(q *store.Queries) *Service {
	return &Service{q: q}
}

// Get returns a user by ID.
func (s *Service) Get(ctx context.Context, id pgtype.UUID) (Profile, error) {
	u, err := s.q.GetUser(ctx, id)
	if err != nil {
		return Profile{}, ErrNotFound
	}
	return toProfile(u), nil
}

// GetMe returns the current user's profile.
func (s *Service) GetMe(ctx context.Context, id pgtype.UUID) (Profile, error) {
	return s.Get(ctx, id)
}

// UpdateProfile updates the user's own profile fields.
func (s *Service) UpdateProfile(ctx context.Context, id pgtype.UUID, displayName, bio string) (Profile, error) {
	u, err := s.q.UpdateUserProfile(ctx, store.UpdateUserProfileParams{
		ID:          id,
		DisplayName: displayName,
		Bio:         bio,
	})
	if err != nil {
		return Profile{}, err
	}
	return toProfile(u), nil
}

// SetAvatar sets the user's avatar URL.
func (s *Service) SetAvatar(ctx context.Context, id pgtype.UUID, avatarURL string) error {
	_, err := s.q.SetUserAvatar(ctx, store.SetUserAvatarParams{
		ID:        id,
		AvatarUrl: avatarURL,
	})
	return err
}

// Search returns users matching a query string for autocomplete.
func (s *Service) Search(ctx context.Context, tenantID, query string, limit int32) ([]SearchResult, error) {
	rows, err := s.q.SearchUsers(ctx, store.SearchUsersParams{
		TenantID: tenantID,
		Column2:  pgtype.Text{String: query, Valid: true},
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, SearchResult{
			ID:          uuidToString(r.ID),
			Email:       r.Email,
			Name:        r.Name,
			DisplayName: r.DisplayName,
			AvatarURL:   r.AvatarUrl,
		})
	}
	return out, nil
}

// List returns all users for admin listing.
func (s *Service) List(ctx context.Context, tenantID string, limit, offset int32) ([]Profile, int64, error) {
	rows, err := s.q.ListUsers(ctx, store.ListUsersParams{
		TenantID: tenantID,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountUsers(ctx, tenantID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Profile, 0, len(rows))
	for _, u := range rows {
		out = append(out, toProfile(u))
	}
	return out, total, nil
}

// SetAdmin toggles the admin flag on a user.
func (s *Service) SetAdmin(ctx context.Context, id pgtype.UUID, isAdmin bool) error {
	return s.q.SetUserAdmin(ctx, store.SetUserAdminParams{
		ID:      id,
		IsAdmin: isAdmin,
	})
}

func toProfile(u store.User) Profile {
	// pgtype.Timestamptz.Value() returns a time.Time, not a string, so the
	// obvious .(string) assertion panics — and did, on every request to
	// /api/v1/users/me once a user had a last_seen, taking the profile screen
	// and the members tab down with a 500.
	lastSeen := ""
	if u.LastSeen.Valid {
		lastSeen = u.LastSeen.Time.Format(timeLayout)
	}
	return Profile{
		ID:          uuidToString(u.ID),
		Email:       u.Email,
		Name:        u.Name,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarUrl,
		Bio:         u.Bio,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   u.CreatedAt.Time.Format(timeLayout),
		LastSeen:    lastSeen,
	}
}

// timeLayout matches the format every DTO in the API uses.
const timeLayout = "2006-01-02T15:04:05Z07:00"

func uuidToString(u pgtype.UUID) string {
	v, _ := u.Value()
	return v.(string)
}
