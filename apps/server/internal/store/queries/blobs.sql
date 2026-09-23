-- name: UpsertBlob :exec
INSERT INTO blobs (hash, size)
VALUES ($1, $2)
ON CONFLICT (hash) DO NOTHING;

-- name: GetBlob :one
SELECT * FROM blobs WHERE hash = $1 LIMIT 1;

-- name: IncrBlobRef :exec
UPDATE blobs SET ref_count = ref_count + 1 WHERE hash = $1;

-- name: DecrBlobRef :exec
UPDATE blobs SET ref_count = ref_count - 1 WHERE hash = $1 AND ref_count > 0;

-- name: ListUnreferencedBlobs :many
SELECT * FROM blobs
WHERE ref_count = 0 AND created_at < now() - INTERVAL '1 hour'
LIMIT $1;

-- name: DeleteBlob :exec
-- Only removes the row if it is still unreferenced, so a blob that gained a
-- reference between the sweep's SELECT and its DELETE survives.
DELETE FROM blobs WHERE hash = $1 AND ref_count = 0;

-- name: BlobTotals :one
-- Referenced vs. awaiting-GC blob counts and bytes for /metrics.
SELECT count(*) FILTER (WHERE ref_count > 0)::bigint AS referenced_blobs,
       COALESCE(sum(size) FILTER (WHERE ref_count > 0), 0)::bigint AS referenced_bytes,
       count(*) FILTER (WHERE ref_count = 0)::bigint AS unreferenced_blobs,
       COALESCE(sum(size) FILTER (WHERE ref_count = 0), 0)::bigint AS unreferenced_bytes
FROM blobs;
