-- name: GetLock :one
SELECT * FROM file_locks WHERE node_id = $1 AND expires_at > now() LIMIT 1;

-- name: UpsertLock :exec
INSERT INTO file_locks (node_id, lock_token, expires_at)
VALUES ($1, $2, now() + INTERVAL '30 minutes')
ON CONFLICT (node_id)
DO UPDATE SET lock_token = EXCLUDED.lock_token, expires_at = EXCLUDED.expires_at;

-- name: RefreshLock :exec
UPDATE file_locks SET expires_at = now() + INTERVAL '30 minutes'
WHERE node_id = $1 AND lock_token = $2;

-- name: DeleteLock :exec
DELETE FROM file_locks WHERE node_id = $1 AND lock_token = $2;

-- name: SweepExpiredLocks :exec
DELETE FROM file_locks WHERE expires_at < now();
