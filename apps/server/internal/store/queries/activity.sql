-- name: RecordActivity :exec
INSERT INTO activity_events (space_id, actor_id, node_id, kind, node_name, detail)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListSpaceActivity :many
SELECT
  a.id, a.space_id, a.actor_id, a.node_id, a.kind, a.node_name, a.detail, a.created_at,
  u.name AS actor_name, u.display_name AS actor_display_name, u.email AS actor_email,
  s.name AS space_name
FROM activity_events a
JOIN users u ON u.id = a.actor_id
JOIN spaces s ON s.id = a.space_id
WHERE a.space_id = $1
ORDER BY a.created_at DESC
LIMIT $2;

-- name: ListMyActivity :many
-- Everything that happened in any space the caller belongs to — the Home
-- feed. Scoped by membership, not by actor: you want to see what your
-- colleagues did, not only what you did.
SELECT
  a.id, a.space_id, a.actor_id, a.node_id, a.kind, a.node_name, a.detail, a.created_at,
  u.name AS actor_name, u.display_name AS actor_display_name, u.email AS actor_email,
  s.name AS space_name
FROM activity_events a
JOIN space_members m ON m.space_id = a.space_id AND m.user_id = $1
JOIN users u ON u.id = a.actor_id
JOIN spaces s ON s.id = a.space_id
ORDER BY a.created_at DESC
LIMIT $2;

-- name: ListRecentNodes :many
-- "Recent" is the caller's own touches, most recently touched first, one row
-- per node. DISTINCT ON collapses repeated edits of the same file into its
-- latest, which is what "recent" means to a person. A null space_id spans
-- every space the caller belongs to; a set one scopes the feed to it.
SELECT DISTINCT ON (n.id)
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  s.name AS space_name,
  COALESCE(p.name, '')::text AS parent_name,
  a.kind AS last_action,
  a.created_at AS last_action_at
FROM activity_events a
JOIN nodes n ON n.id = a.node_id
JOIN space_members m ON m.space_id = n.space_id AND m.user_id = $1
JOIN spaces s ON s.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN nodes p ON p.id = n.parent_id AND p.deleted_at IS NULL
WHERE a.actor_id = $1 AND n.deleted_at IS NULL AND n.kind = 'file'
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
ORDER BY n.id, a.created_at DESC;

-- name: SumSpaceUsage :one
-- Total bytes of the current version of every live file in a space — the
-- figure spaces.used_bytes is supposed to hold.
SELECT COALESCE(SUM(v.size), 0)::bigint AS total
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
WHERE n.space_id = $1 AND n.deleted_at IS NULL;

-- name: SumSpaceUsageByCategory :many
-- Usage split by broad MIME family, for the storage breakdown. The bucketing
-- lives in SQL so the figures and the total come from one pass over the data.
SELECT
  CASE
    WHEN v.mime LIKE 'video/%' THEN 'video'
    WHEN v.mime LIKE 'image/%' THEN 'image'
    WHEN v.mime LIKE 'audio/%' THEN 'audio'
    WHEN v.mime LIKE 'text/%'
      OR v.mime LIKE 'application/pdf'
      OR v.mime LIKE 'application/vnd.openxmlformats%'
      OR v.mime LIKE 'application/vnd.oasis%'
      OR v.mime LIKE 'application/ms%' THEN 'document'
    ELSE 'other'
  END AS category,
  COALESCE(SUM(v.size), 0)::bigint AS bytes,
  COUNT(*)::bigint AS files
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
JOIN space_members m ON m.space_id = n.space_id AND m.user_id = $1
WHERE n.deleted_at IS NULL
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
GROUP BY category
ORDER BY bytes DESC;
