package gc

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"

	"github.com/zeebo/blake3"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
)

// Rows fetched per scrub run; the byte budget normally stops it sooner.
const scrubBatch = 500

// ScrubReport is what one scrub run found.
type ScrubReport struct {
	Checked int
	Bytes   int64
	Damaged int
}

// Scrub re-hashes stored blobs, least recently verified first, until it has
// read budget bytes (always at least one blob, so a blob larger than the
// budget is still reached).
//
// A blob is damaged when its bytes are missing or no longer hash to their
// name — disk rot, a torn write after a power cut, a hand in the data
// directory. Damage is recorded on the row (damaged_at) rather than acted on:
// nothing can repair bytes the store no longer has, but the next upload of
// the same content overwrites the copy instead of deduplicating against it,
// and the count is exported as pphub_blobs_damaged for an alert.
func (w *Worker) Scrub(ctx context.Context, budget int64) (ScrubReport, error) {
	var rep ScrubReport
	rows, err := w.q.ListBlobsToVerify(ctx, scrubBatch)
	if err != nil {
		return rep, err
	}
	for _, b := range rows {
		if rep.Checked > 0 && rep.Bytes >= budget {
			break
		}
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		intact, err := w.check(ctx, b.Hash, b.Size)
		if err != nil {
			return rep, err
		}
		if intact == nil {
			continue // unreadable just now; not evidence either way
		}
		rep.Checked++
		rep.Bytes += b.Size
		if !*intact {
			rep.Damaged++
		}
	}
	return rep, nil
}

// check verifies one blob and records the verdict on its row. A nil verdict
// means the read failed for a reason that says nothing about the bytes — a
// timeout, a permission error — so nothing is recorded and it is retried on
// a later pass.
func (w *Worker) check(ctx context.Context, hash string, size int64) (*bool, error) {
	intact, err := w.verify(ctx, hash, size)
	if err != nil {
		slog.WarnContext(ctx, "blob scrub: read failed", "hash", short(hash), "err", err)
		return nil, nil
	}
	if intact {
		err = w.q.MarkBlobVerified(ctx, hash)
	} else {
		slog.ErrorContext(ctx, "blob scrub: stored bytes are missing or damaged", "hash", short(hash), "size", size)
		err = w.q.MarkBlobDamaged(ctx, hash)
	}
	return &intact, err
}

// verify reports whether the stored bytes for hash exist, are size long and
// hash to hash.
func (w *Worker) verify(ctx context.Context, hash string, size int64) (bool, error) {
	rc, err := w.blobs.Get(ctx, hash)
	if errors.Is(err, blob.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer rc.Close()
	h := blake3.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return false, err
	}
	return n == size && hex.EncodeToString(h.Sum(nil)) == hash, nil
}
