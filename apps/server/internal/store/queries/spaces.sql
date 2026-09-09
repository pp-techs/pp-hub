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
SELECT m.*, u.email, u.name AS user_name
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;

-- name: GetMemberRole :one
SELECT role FROM space_members WHERE space_id = $1 AND user_id = $2 LIMIT 1;

-- name: UpdateSpaceUsedBytes :exec
UPDATE spaces SET used_bytes = $2 WHERE id = $1;
