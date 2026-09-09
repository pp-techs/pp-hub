-- name: CreateShare :one
INSERT INTO shares (node_id, kind, token_hash, grantee_user_id, perm, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetShareByToken :one
SELECT * FROM shares
WHERE token_hash = $1
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;

-- name: ListSharesForNode :many
SELECT * FROM shares WHERE node_id = $1 ORDER BY created_at DESC;

-- name: DeleteShare :exec
DELETE FROM shares WHERE id = $1;

-- name: FindDirectSharesForUser :many
-- Direct shares granted to $2 (user) on $1 (node) or any of its ancestors
-- (ancestors resolved in Go; this query takes a list of node ids).
SELECT * FROM shares
WHERE kind = 'direct'
  AND grantee_user_id = $2
  AND node_id = ANY($1::uuid[])
  AND (expires_at IS NULL OR expires_at > now());

-- name: GetDirectShare :one
SELECT * FROM shares
WHERE kind = 'direct' AND node_id = $1 AND grantee_user_id = $2
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;
