package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

const (
	backfillBatch = 200
	// backfillPause throttles enqueueing so a large sweep does not bury live
	// uploads. Jobs are claimed oldest-first with no priority lane, so every
	// batch enqueued is a batch a fresh upload waits behind.
	backfillPause = 250 * time.Millisecond
)

// Backfill enqueues preview and text-extraction jobs for existing files that
// never got them.
//
// Enqueue only ever ran on upload, so widening the pipeline leaves the whole
// existing drive blank — which reads as a broken feature rather than a missing
// backfill. This is the documented step after first deploying cmd/worker.
//
// Safe to re-run: the queries skip versions that already have their output or
// an in-flight job, and process is idempotent through its upserts.
func Backfill(ctx context.Context, q *store.Queries) (int, error) {
	total := 0
	for _, previewKind := range []string{PreviewThumbnail, PreviewText} {
		n, err := backfillKind(ctx, q, previewKind)
		total += n
		if err != nil {
			return total, err
		}
	}
	n, err := BackfillText(ctx, q)
	total += n
	if err != nil {
		return total, err
	}
	slog.InfoContext(ctx, "backfill complete", "enqueued", total)
	return total, nil
}

// BackfillText enqueues text extraction for current file versions whose
// content is not indexed yet. The API server runs it at startup — the
// extractors are pure Go and live there, and a drive upgraded to content
// search should not stay unsearchable until an operator runs --backfill.
func BackfillText(ctx context.Context, q *store.Queries) (int, error) {
	var after pgtype.UUID
	if err := after.Scan("00000000-0000-0000-0000-000000000000"); err != nil {
		return 0, err
	}
	mimes := slices.Sorted(maps.Keys(extractableMimes))
	kinds := slices.Compact(slices.Sorted(maps.Values(extractableMimes)))

	enqueued := 0
	for {
		rows, err := q.VersionsMissingText(ctx, store.VersionsMissingTextParams{
			Mimes: mimes,
			Kinds: kinds,
			After: after,
			Lim:   backfillBatch,
		})
		if err != nil {
			return enqueued, fmt.Errorf("scan for unindexed text: %w", err)
		}
		if len(rows) == 0 {
			return enqueued, nil
		}
		for _, row := range rows {
			after = row.ID
			payload, _ := json.Marshal(Payload{
				NodeVersionID: uuidStr(row.ID),
				NodeID:        uuidStr(row.NodeID),
				BlobHash:      row.BlobHash,
				Mime:          row.Mime,
				Name:          row.Name,
			})
			kind := extractKindForMime(row.Mime)
			if _, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: kind, Payload: payload}); err != nil {
				return enqueued, fmt.Errorf("enqueue %s: %w", row.Name, err)
			}
			enqueued++
		}
		slog.InfoContext(ctx, "backfill progress", "kind", "extract", "enqueued", enqueued)

		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		case <-time.After(backfillPause):
		}
	}
}

func backfillKind(ctx context.Context, q *store.Queries, previewKind string) (int, error) {
	var after pgtype.UUID
	if err := after.Scan("00000000-0000-0000-0000-000000000000"); err != nil {
		return 0, err
	}

	enqueued := 0
	for {
		rows, err := q.VersionsMissingPreview(ctx, store.VersionsMissingPreviewParams{
			PreviewKind: previewKind,
			After:       after,
			Lim:         backfillBatch,
		})
		if err != nil {
			return enqueued, fmt.Errorf("scan for missing %s: %w", previewKind, err)
		}
		if len(rows) == 0 {
			return enqueued, nil
		}

		for _, row := range rows {
			after = row.ID
			jobKind := kindForMime(row.Mime)
			// The scan is per preview kind, but a MIME maps to a job kind
			// first: skip rows whose renderer produces a different artifact
			// (a PNG turns up in the "text" scan too, and vice versa).
			if jobKind == "" || previewKindFor(jobKind) != previewKind {
				continue
			}
			payload, _ := json.Marshal(Payload{
				NodeVersionID: uuidStr(row.ID),
				NodeID:        uuidStr(row.NodeID),
				BlobHash:      row.BlobHash,
				Mime:          row.Mime,
				Name:          row.Name,
			})
			if _, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: jobKind, Payload: payload}); err != nil {
				return enqueued, fmt.Errorf("enqueue %s: %w", row.Name, err)
			}
			enqueued++
		}
		slog.InfoContext(ctx, "backfill progress", "preview_kind", previewKind, "enqueued", enqueued)

		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		case <-time.After(backfillPause):
		}
	}
}
