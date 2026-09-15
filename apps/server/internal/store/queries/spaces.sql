-- name: CreateSpace :one
INSERT INTO spaces (name, owner_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetSpace :one
SELECT * FROM spaces WHERE id = $1 LIMIT 1;

-- name: ListMySpaces :many
SELECT s.* FROM spaces s
JOIN space_members m ON m.space_id = s.id
WHERE m.user_id = $1
ORDER BY s.created_at;

-- name: AddSpaceMember :exec
INSERT INTO space_members (space_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (space_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RemoveSpaceMember :exec
DELETE FROM space_members WHERE space_id = $1 AND user_id = $2;

-- name: ListSpaceMembers :many
SELECT m.space_id, m.user_id, m.role, m.created_at, u.email, u.name AS user_name, u.display_name, u.avatar_url
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;

-- name: GetMemberRole :one
SELECT role FROM space_members WHERE space_id = $1 AND user_id = $2 LIMIT 1;

-- name: UpdateSpaceUsedBytes :exec
UPDATE spaces SET used_bytes = $2 WHERE id = $1;

-- name: ListSpaceMembersDetailed :many
-- Members with the two figures the settings table shows that a plain member
-- row cannot: how many live files they own here, and when they were last seen.
SELECT
  m.space_id, m.user_id, m.role, m.created_at,
  u.email, u.name AS user_name, u.display_name, u.avatar_url, u.last_seen,
  (SELECT COUNT(*) FROM nodes n
    WHERE n.space_id = m.space_id AND n.created_by = m.user_id
      AND n.deleted_at IS NULL AND n.kind = 'file')::bigint AS files_owned
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;
