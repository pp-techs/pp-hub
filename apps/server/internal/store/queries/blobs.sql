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
