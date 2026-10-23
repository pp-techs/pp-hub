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

-- name: AcquireLock :execrows
-- Takes the lock for token, or refreshes it when token already holds it, and
-- reports 0 rows when somebody else holds a live one. Never steals: unlike
-- UpsertLock, a live lock under another token is left alone.
INSERT INTO file_locks (node_id, lock_token, expires_at)
VALUES ($1, $2, now() + INTERVAL '30 minutes')
ON CONFLICT (node_id) DO UPDATE
SET lock_token = EXCLUDED.lock_token, expires_at = EXCLUDED.expires_at
WHERE file_locks.lock_token = EXCLUDED.lock_token OR file_locks.expires_at <= now();

-- name: DeleteLocksWithPrefix :execrows
-- Releases every lock whose token starts with the prefix — the collab rooms'
-- locks at startup, when no room can be resident yet (one server instance).
DELETE FROM file_locks WHERE starts_with(lock_token, @prefix::text);

-- name: ListOfficeEditorLockTokens :many
-- The live locks OnlyOffice holds on open documents. Its lock id is also the
-- key of the document's editing session, which is what the command service
-- addresses; WOPI lock ids are minted as "wopi_<random>". Collab rooms and
-- WebDAV clients hold other tokens and have no OnlyOffice session.
SELECT lock_token FROM file_locks
WHERE expires_at > now() AND starts_with(lock_token, 'wopi_');
