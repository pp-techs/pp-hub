-- name: EnqueueJob :one
INSERT INTO jobs (kind, payload)
VALUES ($1, $2)
RETURNING *;

-- name: ClaimJobs :many
-- Atomically claim up to @lim pending jobs whose kind this process can handle.
--
-- The kinds filter is what makes cmd/worker optional: a server built without
-- the rasterizers never claims a render:pdf, so the job stays pending instead
-- of burning its five attempts and landing in 'failed' permanently. Deploy the
-- worker later and the backlog drains on the next tick.
--
-- run_at is also set to now(), so for a 'running' row it means "claimed at".
-- ReapStuckJobs needs that: without it a backfilled job whose run_at is hours
-- old would look stale the instant it was claimed, and a second worker could
-- pick it up while the first is still rendering.
UPDATE jobs SET status = 'running', attempts = attempts + 1, run_at = now()
WHERE id IN (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_at <= now()
      AND kind = ANY(@kinds::text[])
    ORDER BY run_at
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ReapStuckJobs :execrows
-- Recover jobs whose worker died mid-flight.
--
-- ClaimJobs flips a row to 'running' and nothing else ever moves it. A worker
-- killed by OOM, a container stop or a deploy therefore strands its jobs in
-- 'running' forever: never retried, never failed, invisible. This is the only
-- thing that puts them back.
--
-- A row is stale when it has been running far longer than any render can take
-- (renderTimeout is 30s), so this cannot race a worker that is still busy.
-- Jobs that have already burned their attempts go to 'failed' rather than
-- looping, and last_error says why.
UPDATE jobs
SET status = CASE
        WHEN attempts >= @max_attempts::int THEN 'failed'::job_status
        ELSE 'pending'::job_status
    END,
    last_error = 'worker stopped before finishing; job reclaimed',
    run_at = now()
WHERE status = 'running'
  AND run_at < now() - make_interval(secs => @lease_seconds::int);

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

-- name: VersionsMissingPreview :many
-- Current versions of live files that have no preview of the given kind and no
-- job already in flight for them. Backfill only — see cmd/worker --backfill.
--
-- Only current versions are considered: handlePreview serves
-- nodes.current_version_id, so rendering superseded versions would burn CPU on
-- blobs no endpoint can reach.
--
-- Keyset-paginated on v.id; pass the last id returned as @after (all-zero UUID
-- to start). The NOT EXISTS scans jobs, which is acceptable for a one-shot
-- admin sweep but is why this is not on any hot path.
SELECT v.id, v.node_id, v.blob_hash, v.mime, n.name
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN previews p ON p.node_version_id = v.id AND p.kind = @preview_kind
WHERE n.kind = 'file'
  AND n.deleted_at IS NULL
  AND p.node_version_id IS NULL
  AND v.id > @after
  AND NOT EXISTS (
      SELECT 1 FROM jobs j
      WHERE j.payload->>'node_version_id' = v.id::text
        AND j.status IN ('pending', 'running')
  )
ORDER BY v.id
LIMIT @lim;

-- name: JobQueueStats :many
-- Per-kind backlog for /metrics. 'done' rows are excluded: they accumulate
-- forever and say nothing about health. The collector caches this for 30s.
SELECT kind, status::text AS status, count(*)::bigint AS jobs,
       COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 AS oldest_age_seconds
FROM jobs
WHERE status IN ('pending', 'running', 'failed')
GROUP BY kind, status;
