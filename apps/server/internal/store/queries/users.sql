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
SELECT id, email, name, display_name, avatar_url
FROM users
WHERE tenant_id = $1
  AND (
    email ILIKE '%' || $2 || '%'
    OR name ILIKE '%' || $2 || '%'
    OR display_name ILIKE '%' || $2 || '%'
  )
ORDER BY
  CASE
    WHEN email ILIKE $2 || '%' THEN 0
    WHEN display_name ILIKE $2 || '%' THEN 1
    WHEN name ILIKE $2 || '%' THEN 2
    ELSE 3
  END,
  display_name, email
LIMIT $3;

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
SELECT m.user_id, m.role, m.created_at, u.email, u.name, u.display_name, u.avatar_url
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;

-- name: IsSpaceMember :one
SELECT EXISTS(SELECT 1 FROM space_members WHERE space_id = $1 AND user_id = $2);

