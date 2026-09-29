package users

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

// storedAvatar is an image already in the CAS, claimed by the store and not
// yet referenced by the account.
type storedAvatar struct {
	url, hash, mime string
}

// storeAvatar sniffs r and writes it to the CAS (see avatar.Store). The
// account's reference is the users.avatar_blob column itself, counted by
// trigger when the swap lands; a swap that loses leaves the blob
// unreferenced, and GC takes it once the store's claim lapses.
func (s *Service) storeAvatar(ctx context.Context, id pgtype.UUID, r io.Reader) (storedAvatar, error) {
	hash, mime, err := avatar.Store(ctx, s.blobs, r)
	if err != nil {
		return storedAvatar{}, err
	}
	// The query suffix exists so a new avatar is not masked by the browser's
	// cache of the old one.
	return storedAvatar{
		url:  fmt.Sprintf("/api/v1/users/%s/avatar?v=%s", uuidToString(id), hash[:12]),
		hash: hash,
		mime: mime,
	}, nil
}

// UploadAvatar stores a person's own avatar and points their profile at it,
// returning the URL it is served from.
//
// The swap is conditional on the blob the account held when this started, so
// of two concurrent uploads exactly one lands and the other is told so.
func (s *Service) UploadAvatar(ctx context.Context, id pgtype.UUID, r io.Reader) (string, error) {
	prev, _, err := s.Avatar(ctx, id)
	if err != nil {
		return "", err
	}
	a, err := s.storeAvatar(ctx, id, r)
	if err != nil {
		return "", err
	}
	_, err = s.q.SetUserAvatar(ctx, store.SetUserAvatarParams{
		ID: id, AvatarUrl: a.url, AvatarBlob: a.hash, AvatarMime: a.mime, PrevBlob: prev,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", avatar.ErrAvatarConflict
	}
	if err != nil {
		return "", err
	}
	return a.url, nil
}
