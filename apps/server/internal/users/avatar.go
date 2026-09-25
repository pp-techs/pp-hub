package users

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// AvatarMaxBytes bounds an avatar from either source. Generous for an image
// that is displayed at 40px.
const AvatarMaxBytes = 5 << 20

var (
	// ErrNotImage: the bytes are not one of avatarMimes.
	ErrNotImage = errors.New("an avatar must be a PNG, JPEG, GIF or WebP image")
	// ErrAvatarConflict: the avatar changed between reading it and replacing
	// it, and this write lost. Nothing was changed.
	ErrAvatarConflict = errors.New("avatar changed concurrently")
)

// avatarMimes are the types an avatar may be. Sniffed from the bytes, never
// taken from a filename or a Content-Type header: the file is served back
// with this value, so whoever could choose it could serve HTML — or an SVG
// with script in it — from our origin.
var avatarMimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// storedAvatar is an image already in the CAS, claimed by the store and not
// yet referenced by the account.
type storedAvatar struct {
	url, hash, mime string
}

// storeAvatar sniffs r and writes it to the CAS. The account's reference is
// the users.avatar_blob column itself, counted by trigger when the swap
// lands; a swap that loses leaves the blob unreferenced, and GC takes it once
// the store's claim lapses.
func (s *Service) storeAvatar(ctx context.Context, id pgtype.UUID, r io.Reader) (storedAvatar, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return storedAvatar{}, err
	}
	head = head[:n]
	mime := http.DetectContentType(head)
	if !avatarMimes[mime] {
		return storedAvatar{}, ErrNotImage
	}

	hash, _, err := s.blobs.Put(ctx, io.MultiReader(bytes.NewReader(head), r))
	if err != nil {
		return storedAvatar{}, fmt.Errorf("blob put: %w", err)
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
		return "", ErrAvatarConflict
	}
	if err != nil {
		return "", err
	}
	return a.url, nil
}
