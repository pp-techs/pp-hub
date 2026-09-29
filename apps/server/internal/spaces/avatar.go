package spaces

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/avatar"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// AvatarURL is the URL a space's picture is served from, "" when it has
// none. The ?v= suffix is the blob prefix, so a new picture is not masked by
// the browser's cache of the old one.
func AvatarURL(id pgtype.UUID, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("/api/v1/spaces/%s/avatar?v=%s", uuidString(id), hash[:12])
}

// Avatar returns a space's avatar blob and mime, ("", "", nil) when it has
// none. The blob is the reference: spaces.avatar_blob is counted by trigger
// and the bytes live in the CAS.
func (s *Service) Avatar(ctx context.Context, id pgtype.UUID) (hash, mime string, err error) {
	row, err := s.q.GetSpaceAvatar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return row.AvatarBlob, row.AvatarMime, nil
}

// UploadAvatar stores a space's picture and points the space at it,
// returning the URL it is served from.
//
// The swap is conditional on the blob the space held when this started, so
// of two concurrent uploads exactly one lands and the other is told so —
// the trigger on spaces moves the reference either way.
func (s *Service) UploadAvatar(ctx context.Context, id pgtype.UUID, r io.Reader) (string, error) {
	prev, _, err := s.Avatar(ctx, id)
	if err != nil {
		return "", err
	}
	hash, mime, err := avatar.Store(ctx, s.blobs, r)
	if err != nil {
		return "", err
	}
	_, err = s.q.SetSpaceAvatar(ctx, store.SetSpaceAvatarParams{
		ID: id, AvatarBlob: hash, AvatarMime: mime, PrevBlob: prev,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", avatar.ErrAvatarConflict
	}
	if err != nil {
		return "", err
	}
	return AvatarURL(id, hash), nil
}

// ClearAvatar removes a space's picture and falls back to the initials
// everywhere. Idempotent: a space without one is the state it leaves.
func (s *Service) ClearAvatar(ctx context.Context, id pgtype.UUID) error {
	prev, _, err := s.Avatar(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.q.SetSpaceAvatar(ctx, store.SetSpaceAvatarParams{
		ID: id, AvatarBlob: "", AvatarMime: "", PrevBlob: prev,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return avatar.ErrAvatarConflict
	}
	return err
}

func uuidString(u pgtype.UUID) string {
	v, _ := u.Value()
	return v.(string)
}
