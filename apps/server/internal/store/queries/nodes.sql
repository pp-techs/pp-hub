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
UPDATE nodes SET deleted_at = now(), deleted_by = $2
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

-- name: GetNodeAny :one
-- Like GetNode but does not filter out soft-deleted rows. Trash operations
-- need to resolve a node precisely because it is deleted.
SELECT * FROM nodes WHERE id = $1 LIMIT 1;

-- name: RestoreNode :exec
-- Clears the tombstone, and re-parents/renames in the same statement so a
-- restore that collides with a live sibling can land under a free name.
UPDATE nodes SET deleted_at = NULL, parent_id = $2, name = $3
WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: PurgeNode :exec
-- Hard delete. Descendants and versions follow via ON DELETE CASCADE; blob
-- ref counts are decremented separately by the caller.
DELETE FROM nodes WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: ListDeletedIDs :many
SELECT id FROM nodes WHERE space_id = $1 AND deleted_at IS NOT NULL;

-- name: ListSubtreeBlobHashes :many
-- Every blob referenced by the subtree rooted at $1, one row per version, so
-- a purge can decrement ref_count exactly as many times as it was incremented.
WITH RECURSIVE sub(node_id) AS (
    SELECT r.id FROM nodes r WHERE r.id = $1
    UNION ALL
    SELECT n.id FROM nodes n JOIN sub ON n.parent_id = sub.node_id
)
SELECT v.blob_hash FROM node_versions v JOIN sub ON v.node_id = sub.node_id;

-- name: ListDeletedWithMeta :many
-- Trash listing: node + current version + owner, plus the original location
-- (ancestor names joined with " / ") and a direct-child count for folders.
WITH RECURSIVE anc AS (
    SELECT n.id AS origin, n.parent_id, n.name, 0 AS depth
    FROM nodes n
    WHERE n.space_id = $1 AND n.deleted_at IS NOT NULL
    UNION ALL
    SELECT a.origin, p.parent_id, p.name, a.depth + 1
    FROM anc a JOIN nodes p ON p.id = a.parent_id
),
loc AS (
    SELECT origin, string_agg(name, ' / ' ORDER BY depth DESC) AS path
    FROM anc WHERE depth > 0 GROUP BY origin
)
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  COALESCE(loc.path, '')::text AS location,
  (SELECT COUNT(*) FROM nodes c WHERE c.parent_id = n.id AND c.deleted_at IS NULL) AS child_count,
  COALESCE(d.display_name, d.name, d.email, '')::text AS deleted_by_name,
  -- NULL when the space keeps its trash indefinitely.
  (CASE WHEN sp.trash_retention_days > 0
        THEN n.deleted_at + (sp.trash_retention_days * INTERVAL '1 day')
   END)::timestamptz AS purges_at
FROM nodes n
JOIN spaces sp ON sp.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN users d ON d.id = n.deleted_by
LEFT JOIN loc ON loc.origin = n.id
WHERE n.space_id = $1 AND n.deleted_at IS NOT NULL
ORDER BY n.deleted_at DESC;

-- name: SearchNodes :many
-- Name search across every space the caller belongs to, or one space when
-- $2 is supplied. Ranked by trigram similarity so the closest name comes
-- first, with an exact prefix match promoted above a mid-string one.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  s.name AS space_name,
  COALESCE(p.name, '')::text AS parent_name
FROM nodes n
JOIN space_members m ON m.space_id = n.space_id AND m.user_id = $1
JOIN spaces s ON s.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN nodes p ON p.id = n.parent_id AND p.deleted_at IS NULL
WHERE n.deleted_at IS NULL
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
  AND (sqlc.narg('kind')::node_kind IS NULL OR n.kind = sqlc.narg('kind')::node_kind)
  AND (sqlc.narg('owner_id')::uuid IS NULL OR n.created_by = sqlc.narg('owner_id')::uuid)
  -- Modified time is the current version's timestamp for a file and the
  -- node's own for a folder, so the filter has to consider both.
  AND (sqlc.narg('modified_after')::timestamptz IS NULL
       OR COALESCE(v.created_at, n.created_at) >= sqlc.narg('modified_after')::timestamptz)
  AND n.name ILIKE '%' || sqlc.arg('query')::text || '%'
ORDER BY
  (lower(n.name) LIKE lower(sqlc.arg('query')::text) || '%') DESC,
  similarity(n.name, sqlc.arg('query')::text) DESC,
  n.kind,
  n.name
LIMIT sqlc.arg('result_limit');

-- name: StarNode :exec
INSERT INTO node_stars (user_id, node_id) VALUES ($1, $2)
ON CONFLICT (user_id, node_id) DO NOTHING;

-- name: UnstarNode :exec
DELETE FROM node_stars WHERE user_id = $1 AND node_id = $2;

-- name: ListStarredNodes :many
-- The caller's starred nodes, newest star first. Space membership is rechecked
-- here because a star survives losing access to the space that holds it. A
-- null space_id spans every space; a set one scopes the listing to it.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  s.name AS space_name,
  COALESCE(p.name, '')::text AS parent_name
FROM node_stars st
JOIN nodes n ON n.id = st.node_id
JOIN space_members m ON m.space_id = n.space_id AND m.user_id = st.user_id
JOIN spaces s ON s.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN nodes p ON p.id = n.parent_id AND p.deleted_at IS NULL
WHERE st.user_id = $1 AND n.deleted_at IS NULL
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
ORDER BY st.created_at DESC
LIMIT $2;

-- name: ListStarredIDs :many
-- Which of these node ids the caller has starred, so a listing can render the
-- star state without one query per row.
SELECT node_id FROM node_stars WHERE user_id = $1 AND node_id = ANY($2::uuid[]);

-- name: ListExpiredTrash :many
-- Tombstones past their space's retention window. A retention of 0 disables
-- automatic purging for that space, so those rows never appear here.
SELECT n.id, n.space_id, n.name
FROM nodes n
JOIN spaces s ON s.id = n.space_id
WHERE n.deleted_at IS NOT NULL
  AND s.trash_retention_days > 0
  AND n.deleted_at < now() - (s.trash_retention_days * INTERVAL '1 day')
LIMIT $1;

-- name: TransferNestedOwnership :exec
-- Nodes in $1 created by $2 that sit under somebody else's folder: ownership
-- climbs to the nearest ancestor's creator, so the content stays where it is
-- instead of moving to the archive. RemoveMember runs this before collecting
-- orphaned roots. Tombstones walk and transfer like live nodes, matching the
-- resolver's unfiltered recursive join.
WITH RECURSIVE up(origin, id, parent_id, created_by, depth) AS (
    SELECT n.id, n.id, n.parent_id, n.created_by, 0
    FROM nodes n
    WHERE n.space_id = @space_id AND n.created_by = @user_id
  UNION ALL
    SELECT u.origin, p.id, p.parent_id, p.created_by, u.depth + 1
    FROM up u JOIN nodes p ON p.id = u.parent_id
    WHERE u.created_by = @user_id
),
new_owner(origin, owner) AS (
    SELECT DISTINCT ON (origin) origin, created_by
    FROM up WHERE created_by <> @user_id
    ORDER BY origin, depth
)
UPDATE nodes n SET created_by = o.owner
FROM new_owner o
WHERE n.id = o.origin;

-- name: ListArchiveRoots :many
-- Topmost nodes in $1 still created by $2 after TransferNestedOwnership:
-- roots. Every chain that never left the departed member's ownership tops
-- out at one of these, so moving each moves everything they owned.
SELECT id FROM nodes
WHERE space_id = $1 AND created_by = $2 AND parent_id IS NULL;

-- name: MoveUnderArchiveRoot :exec
-- Reparent without the live-only filter MoveNode has: a departed member's
-- trashed roots move too, so no node in the space keeps a gone owner.
UPDATE nodes SET parent_id = $2 WHERE id = $1;
