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

-- name: ListTree :many
SELECT * FROM nodes
WHERE space_id = $1
  AND parent_id IS NOT DISTINCT FROM $2
  AND deleted_at IS NULL
ORDER BY kind, name;

-- name: HasChildren :one
SELECT COUNT(*) FROM nodes
WHERE parent_id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListDeleted :many
SELECT * FROM nodes
WHERE space_id = $1 AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC;

-- name: ListChildrenWithMeta :many
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.space_id = $1
  AND n.parent_id IS NOT DISTINCT FROM $2
  AND n.deleted_at IS NULL
ORDER BY n.kind, n.name;

-- name: ListSharedWithMe :many
SELECT DISTINCT n.* FROM nodes n
JOIN spaces s ON s.id = n.space_id
JOIN space_members m ON m.space_id = s.id
WHERE m.user_id = $1
  AND n.deleted_at IS NULL
  AND n.parent_id IS NOT NULL
ORDER BY n.kind, n.name
LIMIT 50;
