-- name: UpdateSpaceSettings :one
UPDATE spaces
SET name = $2, slug = $3, description = $4, default_as_admin = $5, quota_bytes = $6,
    trash_retention_days = $7, allow_public_links = $8, external_sharing = $9
WHERE id = $1
RETURNING *;

-- name: DeleteSpace :exec
DELETE FROM spaces WHERE id = $1;

-- name: SetUserSuspended :exec
UPDATE users SET suspended_at = $2 WHERE id = $1;

-- name: AdminStats :one
SELECT
  (SELECT COUNT(*) FROM users WHERE kind = 'member')::bigint  AS users,
  (SELECT COUNT(*) FROM users WHERE suspended_at IS NOT NULL)::bigint AS suspended_users,
  (SELECT COUNT(*) FROM spaces)::bigint AS spaces,
  (SELECT COUNT(*) FROM nodes WHERE deleted_at IS NULL AND kind = 'file')::bigint AS files,
  (SELECT COUNT(*) FROM nodes WHERE deleted_at IS NOT NULL)::bigint AS trashed,
  (SELECT COALESCE(SUM(size), 0) FROM blobs WHERE ref_count > 0)::bigint AS storage_bytes,
  (SELECT COUNT(*) FROM jobs WHERE status = 'pending')::bigint AS jobs_pending,
  (SELECT COUNT(*) FROM jobs WHERE status = 'failed')::bigint  AS jobs_failed;

-- name: CountUserSpaces :one
SELECT COUNT(*)::bigint FROM space_members WHERE user_id = $1;
