// Package gc reclaims disk space from blobs nothing points at any more.
//
// The content store is deliberately append-only on the write path: uploads add
// blobs and bump ref_count, deletes decrement it, and nothing removes bytes
// inline. That keeps the hot paths simple and makes a crash mid-delete safe —
// ref counts are left too high, never too low. The cost is that unreferenced
// blobs accumulate until something sweeps them, which is this.
package gc

import (
	"context"
	"log/slog"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// How often to sweep. Reclaiming disk is not urgent; running rarely keeps
	// the query off the hot path entirely.
	defaultInterval = 15 * time.Minute
	// Blobs per sweep. Bounded so one pass cannot monopolise the disk.
	defaultBatch = 200
)

type Worker struct {
	q        *store.Queries
	blobs    blob.Store
	interval time.Duration
	batch    int32
}

func NewWorker(q *store.Queries, blobs blob.Store) *Worker {
	return &Worker{q: q, blobs: blobs, interval: defaultInterval, batch: defaultBatch}
}

// Run sweeps until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, bytes, err := w.Sweep(ctx); err != nil {
				slog.Warn("blob gc sweep failed", "err", err)
			} else if n > 0 {
				slog.Info("blob gc reclaimed", "blobs", n, "bytes", bytes)
			}
		}
	}
}

// Sweep deletes one batch of unreferenced blobs and reports what it reclaimed.
//
// `ListUnreferencedBlobs` only returns rows older than an hour, which is the
// safety margin that matters: a blob is written and its ref_count incremented
// in two separate statements, so a brand-new blob is legitimately at zero for
// a moment. Sweeping only what has been unreferenced for an hour means an
// in-flight upload can never be collected out from under itself.
//
// Order matters the other way round from the write path: the row is deleted
// first, then the file. A crash between them leaves a file with no row — a
// disk leak, found by the next full audit — whereas deleting the file first
// would leave a row promising bytes that are gone, which reads as corruption.
func (w *Worker) Sweep(ctx context.Context) (deleted int, reclaimed int64, err error) {
	candidates, err := w.q.ListUnreferencedBlobs(ctx, w.batch)
	if err != nil {
		return 0, 0, err
	}

	for _, b := range candidates {
		// Re-checks ref_count = 0 in the DELETE, so a blob that gained a
		// reference since the SELECT is left alone.
		if err := w.q.DeleteBlob(ctx, b.Hash); err != nil {
			slog.Warn("blob row not deleted", "hash", short(b.Hash), "err", err)
			continue
		}
		if err := w.blobs.Delete(ctx, b.Hash); err != nil {
			// The row is already gone; the file is now orphaned rather than
			// dangerous. Log and carry on rather than failing the sweep.
			slog.Warn("blob file not deleted", "hash", short(b.Hash), "err", err)
			continue
		}
		deleted++
		reclaimed += b.Size
	}
	return deleted, reclaimed, nil
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
