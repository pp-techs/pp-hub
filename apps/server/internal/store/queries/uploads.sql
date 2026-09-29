-- name: CreateUpload :one
INSERT INTO uploads (space_id, parent_id, node_id, attach_to, name, total_size, temp_path, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetUpload :one
-- An upload is only visible to the account that created it, and only until it
-- expires: an expired row is the sweep's, not the client's.
SELECT * FROM uploads
WHERE id = $1 AND created_by = $2 AND expires_at > now()
LIMIT 1;

-- name: AdvanceUpload :exec
-- Records the bytes that are durably on disk and slides the expiry forward,
-- so an upload that is still moving is never swept out from under itself.
UPDATE uploads SET uploaded = $2, expires_at = $3
WHERE id = $1 AND completed_at IS NULL;

-- name: CompleteUpload :exec
UPDATE uploads SET uploaded = total_size, completed_at = now()
WHERE id = $1;

-- name: CompleteAttachmentUpload :exec
-- An attachment upload also records the hash it became, which HEAD reports.
UPDATE uploads SET uploaded = total_size, completed_at = now(), attachment_hash = $2
WHERE id = $1;

-- name: DeleteUpload :exec
DELETE FROM uploads WHERE id = $1;

-- name: PendingUploadBytes :one
-- Bytes already promised to a space by uploads still in flight. Counted
-- against the quota at creation, so ten parallel uploads cannot each pass a
-- check the ten of them together would fail.
SELECT COALESCE(SUM(total_size), 0)::bigint AS total
FROM uploads
WHERE space_id = $1 AND completed_at IS NULL AND expires_at > now();

-- name: DeleteExpiredUploads :many
-- One batch of expired rows, returning where their bytes were staged so the
-- sweep can remove the files after the rows are gone.
DELETE FROM uploads
WHERE id IN (
    SELECT u.id FROM uploads u WHERE u.expires_at <= now() LIMIT $1
)
RETURNING temp_path;
