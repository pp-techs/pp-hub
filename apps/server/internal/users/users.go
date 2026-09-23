// Package users implements user profile management.
// User records are created on first OIDC login by auth.resolveUser, or ahead
// of it by an organization reconcile.
package users

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
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
	// Phone and Gender come from the identity provider and are edited there.
	Phone     string `json:"phone"`
	Gender    string `json:"gender"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
	LastSeen  string `json:"last_seen,omitempty"`
	// Preferences rides on the identity response on purpose: the SPA needs the
	// language before its first paint, and a second round trip for it is a
	// visible flash of English on every load.
	Preferences Preferences `json:"preferences"`
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
	// blobs holds avatar bytes, uploaded or mirrored from the IdP.
	blobs blob.Store
	// http fetches the IdP's picture; pictureFetchTimeout bounds each call.
	http *http.Client
}

func NewService(q *store.Queries, blobs blob.Store) *Service {
	return &Service{q: q, blobs: blobs, http: &http.Client{}}
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

// Avatar returns the blob hash and content type of a user's avatar, or
// ("", "", nil) when they have none.
func (s *Service) Avatar(ctx context.Context, id pgtype.UUID) (hash, mime string, err error) {
	row, err := s.q.GetUserAvatar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return row.AvatarBlob, row.AvatarMime, nil
}

// Search returns users matching a query string for autocomplete.
//
// caller is whose eyes the directory is seen through: the results are the
// accounts sharing at least one space with them, because a space is a company
// and the picker must not double as a tenant-wide address book. unrestricted
// lifts the narrowing for an instance admin, whose console lists every
// account anyway.
func (s *Service) Search(ctx context.Context, tenantID string, caller pgtype.UUID, unrestricted bool, query string, limit int32) ([]SearchResult, error) {
	rows, err := s.q.SearchUsers(ctx, store.SearchUsersParams{
		TenantID:     tenantID,
		Q:            query,
		CallerID:     caller,
		Unrestricted: unrestricted,
		MaxRows:      limit,
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
		Phone:       u.Phone,
		Gender:      u.Gender,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   u.CreatedAt.Time.Format(timeLayout),
		LastSeen:    lastSeen,
		Preferences: DecodePreferences(u.Settings),
	}
}

// timeLayout matches the format every DTO in the API uses.
const timeLayout = "2006-01-02T15:04:05Z07:00"

func uuidToString(u pgtype.UUID) string {
	v, _ := u.Value()
	return v.(string)
}
