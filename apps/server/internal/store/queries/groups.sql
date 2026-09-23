-- name: CreateGroup :one
INSERT INTO groups (space_id, name, description, is_everyone, is_admin, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetEveryoneGroup :one
-- Every space has exactly one, enforced by groups_everyone_uniq.
SELECT * FROM groups WHERE space_id = $1 AND is_everyone LIMIT 1;

-- name: GetAdminGroup :one
-- Every space has exactly one, enforced by groups_admin_uniq. Membership in it
-- IS the admin role — there is no role column anymore (M15).
SELECT * FROM groups WHERE space_id = $1 AND is_admin LIMIT 1;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1 LIMIT 1;

-- name: ListGroups :many
-- The automatic groups sort first; they are the ones reached for most.
SELECT g.*,
       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id = g.id)::int AS member_count,
       (SELECT COUNT(*) FROM shares s
          WHERE s.grantee_group_id = g.id AND s.kind = 'group')::int AS grant_count
FROM groups g
WHERE g.space_id = $1
ORDER BY g.is_everyone DESC, g.is_admin DESC, lower(g.name);

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

-- name: IsSpaceAdmin :one
-- Admin authority is Admin-group membership. This is the only query that
-- answers "is this user an admin of the space".
SELECT EXISTS (
  SELECT 1 FROM group_members gm
  JOIN groups g ON g.id = gm.group_id
  WHERE g.space_id = $1 AND g.is_admin AND gm.user_id = $2
)::bool;

-- name: CountSpaceAdmins :one
-- The last-admin guard: removing or demoting the final Admin-group member
-- would leave the space unadministrable.
SELECT COUNT(*)::int FROM group_members gm
JOIN groups g ON g.id = gm.group_id
WHERE g.space_id = $1 AND g.is_admin;

-- ----------------------------------------------------------------- grants ---

-- name: UpsertGroupGrant :one
-- shares_group_uniq makes re-granting an update rather than a second row.
INSERT INTO shares (node_id, kind, level, can_download, can_share, grantee_group_id, created_by)
VALUES ($1, 'group', $2, $3, $4, $5, $6)
ON CONFLICT (node_id, grantee_group_id) WHERE kind = 'group'
DO UPDATE SET level = EXCLUDED.level, can_download = EXCLUDED.can_download,
              can_share = EXCLUDED.can_share, created_by = EXCLUDED.created_by
RETURNING *;

-- name: DeleteGroupGrant :execrows
DELETE FROM shares
WHERE kind = 'group' AND node_id = $1 AND grantee_group_id = $2;

-- name: ListGrantsForNode :many
-- Group grants on one node, with the group named so the UI need not join.
SELECT s.id, s.level, s.can_download, s.can_share, s.expires_at, s.created_at,
       g.id AS group_id, g.name AS group_name, g.is_everyone, g.is_admin
FROM shares s
JOIN groups g ON g.id = s.grantee_group_id
WHERE s.kind = 'group' AND s.node_id = $1
ORDER BY g.is_everyone DESC, g.is_admin DESC, lower(g.name);

-- name: CountGrantsForGroup :one
SELECT COUNT(*)::int FROM shares WHERE kind = 'group' AND grantee_group_id = $1;

-- name: DeleteSharesByGranteeInSpace :exec
-- Every direct grant naming the departed member: inbound access dies with
-- membership.
DELETE FROM shares s USING nodes n
WHERE s.node_id = n.id AND n.space_id = $1 AND s.grantee_user_id = $2;

-- name: DeleteSharesByCreatorInSpace :exec
-- Every grant the departed member made, links included: their sharing
-- decisions leave with them. Admins recover content — and re-share it — from
-- the archive rather than inheriting the departed member's grants.
DELETE FROM shares s USING nodes n
WHERE s.node_id = n.id AND n.space_id = $1 AND s.created_by = $2;

-- ------------------------------------------------- grant-scoped listings ---

-- name: ListGrantedChildrenWithMeta :many
-- Children of $2 that the caller can reach *directly*: they own them
-- (created_by, and only outside a team folder — team content has no personal
-- owner) or hold a grant on them.
--
-- Only correct when the caller has no access to the parent itself: access
-- inherits downward, so if any ancestor were reachable the whole listing
-- would be visible and the caller would use ListChildrenWithMeta instead.
-- Callers must check the parent's capabilities first.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.space_id = @space_id
  AND n.parent_id IS NOT DISTINCT FROM @parent_id
  AND n.deleted_at IS NULL
  AND ( (n.created_by = @user_id AND n.team_root_id IS NULL)
     OR EXISTS (
        SELECT 1 FROM shares s
        WHERE s.node_id = n.id
          AND (s.expires_at IS NULL OR s.expires_at > now())
          AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
             OR (s.kind = 'group'  AND s.grantee_group_id IN (
                    SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
  )   )
ORDER BY n.kind, n.name;

-- name: ListEntryPoints :many
-- The topmost nodes the caller can reach — the places their access starts.
-- Reachable means granted (directly or via a group) or owned outright
-- (created_by, only when @include_owned — a space's home view). "Shared with
-- me" passes include_owned = false and also drops granted rows the caller
-- owns: your own file is not something shared with you.
--
-- A node sitting under another reachable node is excluded: it is found by
-- walking down from that ancestor, so listing it separately is noise.
-- WITH RECURSIVE must lead the statement; a nested one inside a plain CTE is
-- valid Postgres but sqlc cannot parse it.
WITH RECURSIVE entry(id) AS (
    -- Granted, but only in a space the caller still belongs to. Access is
    -- already gated on membership in NodeAccessCaps; without the same gate
    -- here a departed member whose departure was not yet confirmed kept
    -- seeing the names, sizes and owners of everything shared with them.
    SELECT DISTINCT s.node_id
    FROM shares s
    JOIN nodes gn ON gn.id = s.node_id
    WHERE (s.expires_at IS NULL OR s.expires_at > now())
      AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
         OR (s.kind = 'group'  AND s.grantee_group_id IN (
                SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
      AND EXISTS (
          SELECT 1 FROM space_members sm
          WHERE sm.space_id = gn.space_id AND sm.user_id = @user_id
      )
    UNION
    -- Owned, but only inside a space the caller still belongs to: leaving ends
    -- the reach, and a departed member's roots live in the archive now.
    -- Team content is excluded: `created_by` there is authorship, not access,
    -- so a file inside a team folder is reached through the grant on that
    -- folder and must never surface as its own entry point.
    SELECT n.id FROM nodes n
    WHERE @include_owned::bool AND n.created_by = @user_id AND n.team_root_id IS NULL
      AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
      AND EXISTS (
          SELECT 1 FROM space_members sm
          WHERE sm.space_id = n.space_id AND sm.user_id = @user_id
      )
),
-- Every strict LIVE ancestor of each entry, so the ones sitting underneath
-- another entry can be dropped.
--
-- The climb stops at the first tombstone. A node whose folder was trashed is
-- no longer reachable by walking down to it, so suppressing it here would
-- make it vanish for its own creator while the folder owner decides what to
-- do with the trash — and a purge could then destroy something they never
-- saw. Google Drive draws the same line: trashing a folder does not take
-- other people's access to what is inside it.
--
-- The recursive step climbs to the *parent* of the row it joins. Selecting
-- `p.id` there instead re-emitted the same node forever: with UNION ALL the
-- CTE never terminated, so the first nested owned node made the root listing
-- and the folder tree spin until the client gave up. `UNION` is the belt to
-- that brace — a parent_id cycle can then only produce finitely many pairs.
walk(origin, id) AS (
    SELECT e.id, p.id
    FROM entry e
    JOIN nodes n ON n.id = e.id
    JOIN nodes p ON p.id = n.parent_id
    WHERE p.deleted_at IS NULL
  UNION
    SELECT w.origin, p.id
    FROM walk w
    JOIN nodes c ON c.id = w.id
    JOIN nodes p ON p.id = c.parent_id
    WHERE p.deleted_at IS NULL
)
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM entry e
JOIN nodes n ON n.id = e.id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.deleted_at IS NULL
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
  AND (@include_owned::bool OR n.created_by <> @user_id)
  -- Every walk row is already a strict ancestor — the base step starts at the
  -- parent — so there is no depth to filter on here.
  AND NOT EXISTS (
      SELECT 1 FROM walk w
      JOIN entry e2 ON e2.id = w.id
      WHERE w.origin = e.id
  )
ORDER BY n.kind, n.name
LIMIT 200;

-- name: ListGroupEmailRecipients :many
-- Who a group grant emails: live accounts in the group, minus the granter.
SELECT u.id, u.email, u.settings
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = @group_id
  AND u.suspended_at IS NULL
  AND u.email <> ''
  AND u.id <> @exclude_user_id;
