-- name: UpsertUser :one
INSERT INTO users (oidc_sub, oidc_iss, email, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (oidc_iss, oidc_sub)
DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, last_seen = now()
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2, bio = $3, settings = COALESCE($4, settings)
WHERE id = $1
RETURNING *;

-- name: SetUserAvatar :one
UPDATE users SET avatar_url = $2 WHERE id = $1 RETURNING *;

-- name: SearchUsers :many
-- Autocomplete over accounts the caller may legitimately see. A space is a
-- company, so an ordinary caller only matches people they already share a
-- space with; @unrestricted lifts that for an instance admin, whose console
-- lists every account anyway.
SELECT u.id, u.email, u.name, u.display_name, u.avatar_url
FROM users u
WHERE u.tenant_id = @tenant_id
  AND (
    u.email ILIKE '%' || @q::text || '%'
    OR u.name ILIKE '%' || @q::text || '%'
    OR u.display_name ILIKE '%' || @q::text || '%'
  )
  AND (
    @unrestricted::bool
    OR u.id = @caller_id
    OR EXISTS (
      SELECT 1
      FROM space_members mine
      JOIN space_members theirs ON theirs.space_id = mine.space_id
      WHERE mine.user_id = @caller_id AND theirs.user_id = u.id
    )
  )
ORDER BY
  CASE
    WHEN u.email ILIKE @q::text || '%' THEN 0
    WHEN u.display_name ILIKE @q::text || '%' THEN 1
    WHEN u.name ILIKE @q::text || '%' THEN 2
    ELSE 3
  END,
  u.display_name, u.email
LIMIT @max_rows;

-- name: ListUsers :many
SELECT * FROM users
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1;

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = $2 WHERE id = $1;
-- name: GetSpaceMembers :many
SELECT m.user_id, m.created_at, u.email, u.name, u.display_name, u.avatar_url,
  EXISTS (
    SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id
    WHERE g.space_id = m.space_id AND g.is_admin AND gm.user_id = m.user_id
  ) AS is_admin
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;

-- name: IsSpaceMember :one
SELECT EXISTS(SELECT 1 FROM space_members WHERE space_id = $1 AND user_id = $2);

