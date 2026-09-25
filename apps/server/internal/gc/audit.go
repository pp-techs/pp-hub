package gc

import (
	"context"
	"log/slog"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	// A temp file this old belongs to no write still running: an upload's
	// commit takes seconds, and the store's own temp files live only between
	// the last byte and the rename.
	staleTemp = 24 * time.Hour
	// Hashes per KnownBlobHashes round trip while walking the store.
	auditBatch = 1000
)

// AuditReport is what one audit found and fixed.
type AuditReport struct {
	// TempFiles is how many abandoned temp files were removed.
	TempFiles int
	// Recounted is how many ref_counts disagreed with the referencing rows.
	// The triggers keep them exact, so a non-zero figure is a bug.
	Recounted int
	// Adopted is how many stored blobs had no row and were given one. Such
	// bytes are unreachable to GC until adopted; once they have a row the
	// ordinary sweep collects whichever are unreferenced.
	Adopted int64
}

// Audit reconciles the store with the blobs table: it removes abandoned temp
// files, corrects any ref_count that disagrees with the rows referencing the
// blob, and adopts bytes that have no row.
//
// Bytes without a row are what a crash between writing a blob and recording
// it used to leave, as did every preview before previews were counted. The
// audit never deletes them itself: it gives them a row with their true
// reference count, and the sweep collects the unreferenced ones under the
// same guards as everything else.
//
// A row without bytes is not looked for here — that is the scrub's job, as a
// blob it cannot read.
func (w *Worker) Audit(ctx context.Context) (AuditReport, error) {
	var rep AuditReport
	var err error
	if rep.TempFiles, err = w.blobs.Tidy(ctx, staleTemp); err != nil {
		return rep, err
	}

	fixed, err := w.q.RecountBlobRefs(ctx)
	if err != nil {
		return rep, err
	}
	rep.Recounted = len(fixed)
	for _, f := range fixed {
		slog.ErrorContext(ctx, "blob audit: ref_count was wrong", "hash", short(f.Hash), "now", f.RefCount)
	}

	type walked struct {
		hash string
		size int64
	}
	batch := make([]walked, 0, auditBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		hashes := make([]string, len(batch))
		for i, b := range batch {
			hashes[i] = b.hash
		}
		known, err := w.q.KnownBlobHashes(ctx, hashes)
		if err != nil {
			return err
		}
		have := make(map[string]struct{}, len(known))
		for _, h := range known {
			have[h] = struct{}{}
		}
		for _, b := range batch {
			if _, ok := have[b.hash]; ok {
				continue
			}
			n, err := w.q.AdoptBlob(ctx, store.AdoptBlobParams{Hash: b.hash, Size: b.size})
			if err != nil {
				return err
			}
			rep.Adopted += n
		}
		batch = batch[:0]
		return nil
	}
	err = w.blobs.Walk(ctx, func(hash string, size int64) error {
		batch = append(batch, walked{hash, size})
		if len(batch) == auditBatch {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		return rep, err
	}

	slog.InfoContext(ctx, "blob audit done",
		"temp_files_removed", rep.TempFiles, "ref_counts_fixed", rep.Recounted, "blobs_adopted", rep.Adopted)
	return rep, nil
}
