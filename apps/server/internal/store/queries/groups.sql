-- name: CreateGroup :one
INSERT INTO groups (space_id, name, description, is_everyone, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetEveryoneGroup :one
-- Every space has exactly one, enforced by groups_everyone_uniq.
SELECT * FROM groups WHERE space_id = $1 AND is_everyone LIMIT 1;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1 LIMIT 1;

-- name: ListGroups :many
-- The automatic group sorts first; it is the one an admin reaches for most.
SELECT g.*,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = g.id)::int AS member_count,
       (SELECT COUNT(*) FROM shares s
          WHERE s.grantee_group_id = g.id AND s.kind = 'group')::int AS grant_count
FROM groups g
WHERE g.space_id = $1
ORDER BY g.is_everyone DESC, lower(g.name);

-- name: UpdateGroup :one
UPDATE groups SET name = $2, description = $3 WHERE id = $1 RETURNING *;

-- name: DeleteGroup :exec
DELETE FROM groups WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id)
VALUES ($1, $2)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.email, u.name, u.display_name, u.avatar_url, gm.added_at
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1
ORDER BY lower(COALESCE(NULLIF(u.display_name, ''), u.name, u.email));

-- name: RemoveUserFromSpaceGroups :exec
-- Removing someone from a space must drop them from every group in it.
-- group_members references users, not spaces, so there is no cascade to rely
-- on: without this the person keeps live access through their old groups.
DELETE FROM group_members gm
USING groups g
WHERE gm.group_id = g.id
  AND g.space_id = $1
  AND gm.user_id = $2;

-- name: IsGroupMember :one
SELECT EXISTS (
  SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2
)::bool;

-- ----------------------------------------------------------------- grants ---

-- name: UpsertGroupGrant :one
-- shares_group_uniq makes re-granting an update rather than a second row.
INSERT INTO shares (node_id, kind, perm, grantee_group_id, created_by)
VALUES ($1, 'group', $2, $3, $4)
ON CONFLICT (node_id, grantee_group_id) WHERE kind = 'group'
DO UPDATE SET perm = EXCLUDED.perm, created_by = EXCLUDED.created_by
RETURNING *;

-- name: DeleteGroupGrant :exec
DELETE FROM shares
WHERE kind = 'group' AND node_id = $1 AND grantee_group_id = $2;

-- name: ListGrantsForNode :many
-- Group grants on one node, with the group named so the UI need not join.
SELECT s.id, s.perm, s.expires_at, s.created_at,
       g.id AS group_id, g.name AS group_name, g.is_everyone
FROM shares s
JOIN groups g ON g.id = s.grantee_group_id
WHERE s.kind = 'group' AND s.node_id = $1
ORDER BY g.is_everyone DESC, lower(g.name);

-- name: CountGrantsForGroup :one
SELECT COUNT(*)::int FROM shares WHERE kind = 'group' AND grantee_group_id = $1;

-- ------------------------------------------------- grant-scoped listings ---

-- name: ListGrantedChildrenWithMeta :many
-- Children of $2 that the caller holds a grant on *directly*.
--
-- Only correct when the caller has no access to the parent itself: access
-- inherits downward, so if any ancestor were granted the whole listing would be
-- visible and the caller would use ListChildrenWithMeta instead. Callers must
-- check the parent's rank first.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.space_id = @space_id
  AND n.parent_id IS NOT DISTINCT FROM @parent_id
  AND n.deleted_at IS NULL
  AND EXISTS (
    SELECT 1 FROM shares s
    WHERE s.node_id = n.id
      AND (s.expires_at IS NULL OR s.expires_at > now())
      AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
         OR (s.kind = 'group'  AND s.grantee_group_id IN (
                SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
  )
ORDER BY n.kind, n.name;

-- name: ListEntryPoints :many
-- The topmost nodes the caller holds a grant on, across every space they
-- belong to. This is what "Shared with me" means: the places their access
-- starts. A node whose ancestor is also granted is excluded — it would be
-- reachable by walking down from the ancestor, so listing it as a separate
-- entry point is noise.
-- WITH RECURSIVE must lead the statement; a nested one inside a plain CTE is
-- valid Postgres but sqlc cannot parse it.
WITH RECURSIVE granted AS (
    SELECT DISTINCT s.node_id
    FROM shares s
    WHERE (s.expires_at IS NULL OR s.expires_at > now())
      AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
         OR (s.kind = 'group'  AND s.grantee_group_id IN (
                SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
),
-- Every strict ancestor of each granted node, so the ones sitting underneath
-- another grant can be dropped.
walk(origin, id, parent_id, depth) AS (
    SELECT g.node_id, g.node_id, n.parent_id, 0
    FROM granted g JOIN nodes n ON n.id = g.node_id
  UNION ALL
    SELECT w.origin, p.id, p.parent_id, w.depth + 1
    FROM walk w JOIN nodes p ON p.id = w.parent_id
)
SELECT n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
       n.current_version_id, n.deleted_at, n.created_by, n.created_at
FROM granted g
JOIN nodes n ON n.id = g.node_id
WHERE n.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM walk w
      JOIN granted g2 ON g2.node_id = w.id
      WHERE w.origin = g.node_id AND w.depth > 0
  )
ORDER BY n.kind, n.name
LIMIT 200;
