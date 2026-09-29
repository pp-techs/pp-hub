package users

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/avatar"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// pictureFetchTimeout bounds the one outbound call a sign-in can make on
	// the IdP's behalf. It only happens when the picture changed.
	pictureFetchTimeout = 10 * time.Second
	// maxIdPDetail bounds phone and gender before they reach a TEXT column.
	// Both are short; the IdP is trusted, but not to be tidy.
	maxIdPDetail = 64
)

// IdPProfile is what the identity provider says about a person beyond who
// they are: fields pp-hub shows but does not let anyone edit here. Read from
// the OIDC `picture`, `phone_number` and `gender` claims at sign-in, and from
// Logto's Management API on an organization reconcile.
type IdPProfile struct {
	// Picture is the avatar image's URL. Empty means the IdP has none.
	Picture string
	Phone   string
	Gender  string
}

// SyncIdPProfile mirrors the IdP-owned fields onto u and returns the row as
// it now stands. Only a field that changed is written, so calling this on
// every sign-in and every reconcile costs nothing in the steady state.
//
// The avatar follows the most recent change on either side. A new picture at
// the IdP replaces whatever is shown here, an upload here replaces the IdP's
// until the IdP's changes again, and an IdP that drops its picture clears the
// avatar only if it was the IdP's to begin with.
//
// On error the returned row is the most recent one that was written, so the
// caller can carry on with it: nothing here is a reason to refuse a sign-in.
func (s *Service) SyncIdPProfile(ctx context.Context, u store.User, p IdPProfile) (store.User, error) {
	phone := NormalizePhone(clip(p.Phone, maxIdPDetail))
	gender := clip(strings.TrimSpace(p.Gender), maxIdPDetail)
	if phone != u.Phone || gender != u.Gender {
		next, err := s.q.SetUserIdpDetails(ctx, store.SetUserIdpDetailsParams{
			ID: u.ID, Phone: phone, Gender: gender,
		})
		if err != nil {
			return u, err
		}
		u = next
	}
	return s.syncPicture(ctx, u, strings.TrimSpace(p.Picture))
}

func (s *Service) syncPicture(ctx context.Context, u store.User, picture string) (store.User, error) {
	if u.IdpPicture.Valid && u.IdpPicture.String == picture {
		return u, nil
	}
	seen := pgtype.Text{String: picture, Valid: true}
	record := func() (store.User, error) {
		next, err := s.q.SetUserIdpPicture(ctx, store.SetUserIdpPictureParams{ID: u.ID, IdpPicture: seen})
		if err != nil {
			return u, err
		}
		return next, nil
	}

	switch {
	case picture == "" && (!u.AvatarFromIdp || u.AvatarBlob == ""):
		// Nothing of the IdP's to take away.
		return record()
	case picture == "":
		return s.swapIdPAvatar(ctx, u, storedAvatar{}, seen)
	case !u.IdpPicture.Valid && u.AvatarBlob != "" && !u.AvatarFromIdp:
		// The first picture this account has been seen with, and an upload
		// already in place: the upload predates the sync, so it is the more
		// deliberate of the two. From here on, a change at the IdP wins.
		return record()
	}

	body, err := s.fetchPicture(ctx, picture)
	if err != nil {
		// Not recorded: a network failure is worth another try next time.
		return u, fmt.Errorf("fetch idp picture: %w", err)
	}
	a, err := s.storeAvatar(ctx, u.ID, bytes.NewReader(body))
	if errors.Is(err, avatar.ErrNotImage) {
		// An SVG or an HTML error page, say. Recorded, so the same URL is not
		// fetched again on every sign-in only to be refused again.
		next, recErr := record()
		if recErr != nil {
			return u, recErr
		}
		return next, fmt.Errorf("idp picture %s: %w", picture, err)
	}
	if err != nil {
		return u, err
	}
	return s.swapIdPAvatar(ctx, u, a, seen)
}

// swapIdPAvatar points u at a (or at nothing, for the zero value); see
// UploadAvatar for why it is guarded. The triggers on users move the blob
// references either way.
func (s *Service) swapIdPAvatar(ctx context.Context, u store.User, a storedAvatar, seen pgtype.Text) (store.User, error) {
	next, err := s.q.SetUserIdpAvatar(ctx, store.SetUserIdpAvatarParams{
		ID: u.ID, AvatarUrl: a.url, AvatarBlob: a.hash, AvatarMime: a.mime,
		IdpPicture: seen, PrevBlob: u.AvatarBlob,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return u, avatar.ErrAvatarConflict
	}
	if err != nil {
		return u, err
	}
	return next, nil
}

// fetchPicture downloads the IdP's picture, bounded in time and size.
//
// The URL is the IdP's, which on a self-service IdP means the user's. What
// that allows is a GET from this server that returns nothing to the caller
// unless the answer sniffs as an image — the same reach a link preview has.
// Private addresses are deliberately not refused: an on-premises IdP serving
// its avatars from an internal hostname is the ordinary case, not an attack.
func (s *Service) fetchPicture(ctx context.Context, raw string) ([]byte, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, fmt.Errorf("not an http(s) url: %q", raw)
	}
	ctx, cancel := context.WithTimeout(ctx, pictureFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/png, image/jpeg, image/gif, image/webp")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, avatar.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > avatar.MaxBytes {
		return nil, fmt.Errorf("larger than %d bytes", avatar.MaxBytes)
	}
	return body, nil
}

// NormalizePhone renders a phone number the way OIDC's phone_number claim is
// meant to be written, E.164 with its leading "+". Logto stores the number
// without the plus, and hands it out that way from both the ID token and the
// Management API; anything that is not all digits is left as the IdP gave it.
func NormalizePhone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return s
		}
	}
	return "+" + s
}

// clip cuts s to at most max bytes without splitting a UTF-8 sequence.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
