package gc

import (
	"context"
	"log/slog"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
)

const (
	trashInterval = 1 * time.Hour
	trashBatch    = 100
)

// TrashWorker permanently removes tombstones that have outlived their space's
// retention window.
//
// Retention is opt-in per space and defaults to 0, which disables the sweep —
// no existing space has its trash emptied by this being deployed. A team that
// wants a 30-day bin sets it in space settings, and only then does anything
// here touch their data.
type TrashWorker struct {
	fs       *fs.Service
	interval time.Duration
}

func NewTrashWorker(f *fs.Service) *TrashWorker {
	return &TrashWorker{fs: f, interval: trashInterval}
}

func (w *TrashWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := w.Sweep(ctx); err != nil {
				slog.Warn("trash retention sweep failed", "err", err)
			} else if n > 0 {
				slog.Info("trash retention purged", "nodes", n)
			}
		}
	}
}

// Sweep purges one batch of expired tombstones and returns how many went.
//
// Each purge releases its subtree's blob references; the blob sweep reclaims
// the bytes afterwards. A node already gone — taken by an ancestor's cascade
// earlier in the same batch — is skipped rather than treated as a failure.
func (w *TrashWorker) Sweep(ctx context.Context) (int, error) {
	expired, err := w.fs.ExpiredTrash(ctx, trashBatch)
	if err != nil {
		return 0, err
	}

	purged := 0
	for _, node := range expired {
		// Retention expiry is a background sweep, so a rescued subtree gets no
		// notification beyond its new position at its owner's top level.
		_, err := w.fs.Purge(ctx, node.ID)
		switch {
		case err == nil:
			purged++
		case errorIsGone(err):
			// Removed with an ancestor earlier in this batch.
		default:
			slog.Warn("expired trash not purged", "name", node.Name, "err", err)
		}
	}
	return purged, nil
}

func errorIsGone(err error) bool {
	return err == fs.ErrNotFound || err == fs.ErrNotDeleted
}
