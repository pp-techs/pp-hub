// Package avatar is the one place that decides what an avatar is: the byte
// cap, the image types and the sniff-into-the-CAS step every avatar source
// shares — a person's upload, the IdP's picture and a space's picture all go
// through Store.
package avatar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
)

// MaxBytes bounds an avatar from either source. Generous for an image
// that is displayed at 40px.
const MaxBytes = 5 << 20

var (
	// ErrNotImage: the bytes are not one of mimes.
	ErrNotImage = errors.New("an avatar must be a PNG, JPEG, GIF or WebP image")
	// ErrAvatarConflict: the avatar changed between reading it and replacing
	// it, and this write lost. Nothing was changed.
	ErrAvatarConflict = errors.New("avatar changed concurrently")
)

// mimes are the types an avatar may be. Sniffed from the bytes, never
// taken from a filename or a Content-Type header: the file is served back
// with this value, so whoever could choose it could serve HTML — or an SVG
// with script in it — from our origin.
var mimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// Store sniffs r and writes it to the CAS, returning the hash and the sniffed
// mime. The caller's column is the reference — users.avatar_blob,
// spaces.avatar_blob — counted by trigger when the swap lands; a swap that
// loses leaves the blob unreferenced, and GC takes it once the store's claim
// lapses.
func Store(ctx context.Context, blobs blob.Store, r io.Reader) (hash, mime string, err error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", err
	}
	head = head[:n]
	mime = http.DetectContentType(head)
	if !mimes[mime] {
		return "", "", ErrNotImage
	}

	hash, _, err = blobs.Put(ctx, io.MultiReader(bytes.NewReader(head), r))
	if err != nil {
		return "", "", fmt.Errorf("blob put: %w", err)
	}
	return hash, mime, nil
}
