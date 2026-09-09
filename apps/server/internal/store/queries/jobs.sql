-- name: EnqueueJob :one
INSERT INTO jobs (kind, payload)
VALUES ($1, $2)
RETURNING *;

-- name: ClaimJobs :many
-- Atomically claim up to $1 pending jobs for processing.
UPDATE jobs SET status = 'running', attempts = attempts + 1
WHERE id IN (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_at <= now()
    ORDER BY run_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteJob :exec
UPDATE jobs SET status = 'done' WHERE id = $1;

-- name: FailJob :exec
UPDATE jobs SET status = 'failed', last_error = $2,
    run_at = now() + (INTERVAL '1 minute' * attempts)
WHERE id = $1;

-- name: RequeueJob :exec
-- Retry: back to pending with backoff.
UPDATE jobs SET status = 'pending', last_error = $2,
    run_at = now() + (INTERVAL '1 minute' * attempts)
WHERE id = $1;

-- name: UpsertPreview :exec
INSERT INTO previews (node_version_id, kind, blob_hash)
VALUES ($1, $2, $3)
ON CONFLICT (node_version_id, kind) DO UPDATE SET blob_hash = EXCLUDED.blob_hash;

-- name: GetPreview :one
SELECT * FROM previews WHERE node_version_id = $1 AND kind = $2 LIMIT 1;
