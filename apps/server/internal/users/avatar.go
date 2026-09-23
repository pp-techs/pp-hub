package users

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// storedAvatar is an image already in the CAS, with the reference the account
// is about to hold on it already counted.
type storedAvatar struct {
	url, hash, mime string
}

// storeAvatar sniffs r, writes it to the CAS and takes a reference on it.
//
// Mirrors fs.PutFile's Put → UpsertBlob → IncrBlobRef sequence — skipped when
// the bytes are the blob the account already points at (prev), so ref_count
// is not inflated and gc.Worker.Sweep does not think the blob is briefly
// unreferenced.
func (s *Service) storeAvatar(ctx context.Context, id pgtype.UUID, prev string, r io.Reader) (storedAvatar, error) {
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

	hash, size, err := s.blobs.Put(ctx, io.MultiReader(bytes.NewReader(head), r))
	if err != nil {
		return storedAvatar{}, fmt.Errorf("blob put: %w", err)
	}
	if hash != prev {
		if err := s.q.UpsertBlob(ctx, store.UpsertBlobParams{Hash: hash, Size: size}); err != nil {
			return storedAvatar{}, err
		}
		if err := s.q.IncrBlobRef(ctx, hash); err != nil {
			return storedAvatar{}, err
		}
	}
	// The query suffix exists so a new avatar is not masked by the browser's
	// cache of the old one.
	return storedAvatar{
		url:  fmt.Sprintf("/api/v1/users/%s/avatar?v=%s", uuidToString(id), hash[:12]),
		hash: hash,
		mime: mime,
	}, nil
}

// release drops the account's reference on drop once keep has replaced it.
//
// Called with (prev, next) after a swap lands, and with (next, prev) after it
// loses, to undo storeAvatar's speculative reference. A failure is logged,
// not returned: the swap itself already happened, and a ref_count left too
// high is the safe direction — GC never over-collects.
func (s *Service) release(ctx context.Context, drop, keep string) {
	if drop == "" || drop == keep {
		return
	}
	if err := s.q.DecrBlobRef(ctx, drop); err != nil {
		slog.ErrorContext(ctx, "avatar: decrementing blob ref failed", "blob", drop, "err", err)
	}
}

// UploadAvatar stores a person's own avatar and points their profile at it,
// returning the URL it is served from.
//
// The swap is conditional on the blob the account held when this started.
// Two unguarded writers would both release the same previous blob, and in a
// deduplicating CAS that blob can be a file's content as well.
func (s *Service) UploadAvatar(ctx context.Context, id pgtype.UUID, r io.Reader) (string, error) {
	prev, _, err := s.Avatar(ctx, id)
	if err != nil {
		return "", err
	}
	a, err := s.storeAvatar(ctx, id, prev, r)
	if err != nil {
		return "", err
	}
	_, err = s.q.SetUserAvatar(ctx, store.SetUserAvatarParams{
		ID: id, AvatarUrl: a.url, AvatarBlob: a.hash, AvatarMime: a.mime, PrevBlob: prev,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		s.release(ctx, a.hash, prev)
		return "", ErrAvatarConflict
	}
	if err != nil {
		return "", err
	}
	s.release(ctx, prev, a.hash)
	return a.url, nil
}
