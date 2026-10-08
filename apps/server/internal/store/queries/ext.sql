-- name: CreateExtNode :one
-- A node whose content is an extension's rows. team_root_id is inherited as
-- for any node.
INSERT INTO nodes (space_id, parent_id, kind, ext_kind, name, created_by, team_root_id)
VALUES ($1, $2, 'ext', $3, $4, $5,
        (SELECT p.team_root_id FROM nodes p WHERE p.id = $2))
RETURNING *;

-- name: ListExtState :many
SELECT ext_id, space_id, enabled FROM ext_state;

-- name: ExtEnabledForSpace :one
-- On for a space when the instance switch is on and the space has not
-- switched it off.
SELECT COALESCE((SELECT i.enabled FROM ext_state i
                  WHERE i.ext_id = @ext_id AND i.space_id IS NULL), false)
       AND NOT EXISTS (SELECT 1 FROM ext_state s
                        WHERE s.ext_id = @ext_id AND s.space_id = @space_id
                          AND NOT s.enabled) AS enabled;

-- name: SetExtInstanceState :exec
INSERT INTO ext_state (ext_id, space_id, enabled, updated_by)
VALUES ($1, NULL, $2, $3)
ON CONFLICT (ext_id) WHERE space_id IS NULL
DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: SetExtSpaceState :exec
INSERT INTO ext_state (ext_id, space_id, enabled, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (ext_id, space_id) WHERE space_id IS NOT NULL
DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: ClearExtSpaceState :exec
DELETE FROM ext_state WHERE ext_id = $1 AND space_id = $2;

-- name: ExtEnabledForInstance :one
SELECT COALESCE((SELECT i.enabled FROM ext_state i
                  WHERE i.ext_id = @ext_id AND i.space_id IS NULL), false)::bool AS enabled;

-- name: ListExtSpaceOff :many
-- The spaces that have switched an extension off for themselves.
SELECT space_id FROM ext_state
WHERE ext_id = $1 AND space_id IS NOT NULL AND NOT enabled;

-- name: SearchSpaceUsers :many
-- Members of a space matching a typed name or email, for a picker. Suspended
-- accounts are not offered.
SELECT u.id, u.email, u.name, u.display_name
FROM users u
JOIN space_members m ON m.user_id = u.id
WHERE m.space_id = @space_id
  AND u.suspended_at IS NULL
  AND (@q::text = ''
       OR u.email ILIKE '%' || @q::text || '%'
       OR u.name ILIKE '%' || @q::text || '%'
       OR u.display_name ILIKE '%' || @q::text || '%')
ORDER BY u.display_name, u.email
LIMIT @max_rows;

-- name: GetLiveNodesByIDs :many
-- The live nodes among ids, for an extension that links to nodes.
SELECT id, space_id, kind, ext_kind, name FROM nodes
WHERE id = ANY(@ids::uuid[]) AND deleted_at IS NULL;

-- name: ListExtNodesWithMeta :many
-- Every live node of one extension in a space, wherever it hangs: the
-- candidates for the sidebar's Apps list. The caller filters them by what the
-- viewer can open (reachableRows), so the window is wide rather than ranked.
-- Same columns as ListChildrenWithMeta, so one mapper serves both.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id, n.ext_kind,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.space_id = @space_id
  AND n.kind = 'ext'
  AND n.ext_kind LIKE @ext_prefix::text
  AND n.deleted_at IS NULL
ORDER BY lower(n.name), n.id
LIMIT 2000;
