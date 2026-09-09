-- name: CreateNode :one
INSERT INTO nodes (space_id, parent_id, kind, name, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1 AND deleted_at IS NULL LIMIT 1;

-- name: ListChildren :many
SELECT * FROM nodes
WHERE space_id = $1
  AND parent_id IS NOT DISTINCT FROM $2
  AND deleted_at IS NULL
ORDER BY kind, name;

-- name: GetSpaceRoot :one
SELECT * FROM nodes
WHERE space_id = $1 AND parent_id IS NULL AND deleted_at IS NULL
ORDER BY created_at LIMIT 1;

-- name: RenameNode :exec
UPDATE nodes SET name = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: MoveNode :exec
UPDATE nodes SET parent_id = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: SoftDeleteNode :exec
UPDATE nodes SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetNodeParent :one
-- Returns parent_id of a live node (NULL for roots). Used to walk the chain
-- in Go (cycle check, permission inheritance) without recursive SQL.
SELECT parent_id FROM nodes WHERE id = $1 AND deleted_at IS NULL LIMIT 1;
-- name: AddVersion :one
INSERT INTO node_versions (node_id, blob_hash, size, mime, version_no, created_by)
VALUES ($1, $2, $3, $4,
        (SELECT COALESCE(MAX(version_no), 0) + 1 FROM node_versions WHERE node_id = $1),
        $5)
RETURNING *;

-- name: SetCurrentVersion :exec
UPDATE nodes SET current_version_id = $2 WHERE id = $1;

-- name: ListVersions :many
SELECT * FROM node_versions WHERE node_id = $1 ORDER BY version_no DESC;

-- name: GetVersion :one
SELECT * FROM node_versions WHERE id = $1 LIMIT 1;

-- name: GetCurrentVersion :one
SELECT v.* FROM node_versions v
JOIN nodes n ON n.current_version_id = v.id
WHERE n.id = $1 AND n.deleted_at IS NULL
LIMIT 1;
