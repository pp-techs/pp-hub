-- name: CreateInvitation :one
-- Re-inviting the same address replaces the pending row rather than stacking
-- a second one, so the token in the newest email is the only one that works.
INSERT INTO space_invitations (space_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (space_id, lower(email)) WHERE accepted_at IS NULL
DO UPDATE SET role = EXCLUDED.role, token_hash = EXCLUDED.token_hash,
              invited_by = EXCLUDED.invited_by, expires_at = EXCLUDED.expires_at,
              created_at = now()
RETURNING *;

-- name: ListSpaceInvitations :many
SELECT i.*, u.name AS inviter_name, u.display_name AS inviter_display_name, u.email AS inviter_email
FROM space_invitations i
JOIN users u ON u.id = i.invited_by
WHERE i.space_id = $1 AND i.accepted_at IS NULL
ORDER BY i.created_at DESC;

-- name: GetInvitationByToken :one
SELECT * FROM space_invitations
WHERE token_hash = $1 AND accepted_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;

-- name: AcceptInvitation :exec
UPDATE space_invitations SET accepted_at = now() WHERE id = $1;

-- name: DeleteInvitation :exec
DELETE FROM space_invitations WHERE id = $1 AND space_id = $2;

-- name: UpdateSpaceSettings :one
UPDATE spaces
SET name = $2, slug = $3, description = $4, default_role = $5, quota_bytes = $6,
    trash_retention_days = $7
WHERE id = $1
RETURNING *;

-- name: DeleteSpace :exec
DELETE FROM spaces WHERE id = $1;

-- name: SetUserSuspended :exec
UPDATE users SET suspended_at = $2 WHERE id = $1;

-- name: AdminStats :one
SELECT
  (SELECT COUNT(*) FROM users)::bigint  AS users,
  (SELECT COUNT(*) FROM users WHERE suspended_at IS NOT NULL)::bigint AS suspended_users,
  (SELECT COUNT(*) FROM spaces)::bigint AS spaces,
  (SELECT COUNT(*) FROM nodes WHERE deleted_at IS NULL AND kind = 'file')::bigint AS files,
  (SELECT COUNT(*) FROM nodes WHERE deleted_at IS NOT NULL)::bigint AS trashed,
  (SELECT COALESCE(SUM(size), 0) FROM blobs WHERE ref_count > 0)::bigint AS storage_bytes,
  (SELECT COUNT(*) FROM jobs WHERE status = 'pending')::bigint AS jobs_pending,
  (SELECT COUNT(*) FROM jobs WHERE status = 'failed')::bigint  AS jobs_failed;

-- name: CountUserSpaces :one
SELECT COUNT(*)::bigint FROM space_members WHERE user_id = $1;
