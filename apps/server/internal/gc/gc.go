// Package gc looks after the blob store: it reclaims blobs nothing points at
// any more, reconciles the store with the blobs table, and re-hashes stored
// bytes to find damage before a reader does.
//
// The content store is deliberately append-only on the write path: uploads add
// blobs, references come and go as rows (counted into blobs.ref_count by
// trigger), and nothing removes bytes inline. That keeps the hot paths simple.
// The cost is that unreferenced blobs accumulate until something sweeps them,
// which is this.
package gc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// How often to sweep. Reclaiming disk is not urgent; running rarely keeps
	// the query off the hot path entirely.
	sweepInterval = 15 * time.Minute
	// Blobs per sweep. Bounded so one pass cannot monopolise the disk.
	sweepBatch = 200
	// How often the scrub runs; each run spends a day's budget / 24.
	scrubInterval = time.Hour
	// How often the store is walked against the table. The first audit runs
	// auditDelay after start, off the startup path.
	auditInterval = 24 * time.Hour
	auditDelay    = 10 * time.Minute
)

type Worker struct {
	q     *store.Queries
	blobs blob.Store
	// scrubPerDay is how many bytes the scrub may read in a day; 0 disables it.
	scrubPerDay int64
}

func NewWorker(q *store.Queries, blobs blob.Store, scrubPerDay int64) *Worker {
	return &Worker{q: q, blobs: blobs, scrubPerDay: scrubPerDay}
}

// Run sweeps, scrubs and audits until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()
	scrub := time.NewTicker(scrubInterval)
	defer scrub.Stop()
	audit := time.NewTimer(auditDelay)
	defer audit.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			if n, bytes, err := w.Sweep(ctx); err != nil {
				slog.WarnContext(ctx, "blob gc sweep failed", "err", err)
			} else if n > 0 {
				slog.InfoContext(ctx, "blob gc reclaimed", "blobs", n, "bytes", bytes)
			}
		case <-scrub.C:
			if w.scrubPerDay <= 0 {
				continue
			}
			if _, err := w.Scrub(ctx, w.scrubPerDay/24); err != nil {
				slog.WarnContext(ctx, "blob scrub failed", "err", err)
			}
		case <-audit.C:
			if _, err := w.Audit(ctx); err != nil {
				slog.WarnContext(ctx, "blob audit failed", "err", err)
			}
			audit.Reset(auditInterval)
		}
	}
}

// errKept: the row stopped qualifying for collection between the listing and
// the delete — a write claimed it, or something references it again.
var errKept = errors.New("blob kept")

// Sweep deletes one batch of collectable blobs and reports what it reclaimed.
//
// A blob is collectable when it has been unreferenced for over an hour (from
// released_at, so an old blob just let go waits as long as a new one) and no
// write has claimed it since. The hour is also the window an external backup
// has to copy the blobs a database dump references.
//
// Each blob goes in its own transaction: the row is deleted under its lock,
// then the bytes, then the commit. A write claiming the same hash waits on
// that lock and finds either a live row with its bytes, or no row and no
// bytes — never a row about to lose its file, which is how a freshly
// deduplicated upload could be collected out from under its new reference.
// A failure to remove the bytes rolls the row back, so nothing is forgotten.
func (w *Worker) Sweep(ctx context.Context) (deleted int, reclaimed int64, err error) {
	candidates, err := w.q.ListCollectableBlobs(ctx, sweepBatch)
	if err != nil {
		return 0, 0, err
	}
	for _, b := range candidates {
		size, err := w.collect(ctx, b.Hash)
		if errors.Is(err, errKept) {
			continue
		}
		if err != nil {
			slog.WarnContext(ctx, "blob not collected", "hash", short(b.Hash), "err", err)
			continue
		}
		deleted++
		reclaimed += size
	}
	return deleted, reclaimed, nil
}

func (w *Worker) collect(ctx context.Context, hash string) (int64, error) {
	var size int64
	err := w.q.InTx(ctx, func(q *store.Queries) error {
		var err error
		size, err = q.DeleteCollectableBlob(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return errKept
		}
		if err != nil {
			return err
		}
		return w.blobs.Delete(ctx, hash)
	})
	return size, err
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
