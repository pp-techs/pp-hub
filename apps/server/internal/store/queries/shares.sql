-- name: CreateShare :one
INSERT INTO shares (node_id, kind, token_hash, grantee_user_id, level, can_download, can_share, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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

-- name: NodeAccessCaps :many
-- For each node in $1: the capabilities $2 holds via grants — direct or via a
-- group — on the node itself or any ancestor, plus whether $2 OWNS the node
-- or any ancestor (created_by). Ownership is intrinsic full access, so the
-- owner flag must survive the no-grant case: one row per requested node.
--
-- There is no role floor anymore (M15): an admin who owns nothing and was
-- granted nothing resolves to no capabilities, like anyone else.
--
-- Everything is gated on current space membership. Ownership is intrinsic but
-- not perpetual: leaving a company ends the caller's reach into its tree, and
-- their `created_by` rows stay only so the archive keeps an author. Without
-- this, RemoveMember would file someone's work in the archive while leaving
-- them able to read and edit it.
--
-- The recursive join deliberately does NOT filter deleted_at. GetNodeParent
-- does, which truncates the chain at the first trashed ancestor and loses the
-- grant above it — the reason trash resolves exactly like the live tree.
-- ListDeletedWithMeta walks the same way for the same reason.
--
-- UNION ALL cannot dedup its way out of a parent cycle, and this query gates
-- every node-addressed route: one cycle would hang the whole space. The depth
-- cap is the brake; nodes_no_self_parent_ck keeps the obvious cycle out.
WITH RECURSIVE anc(origin, id, parent_id, depth) AS (
    SELECT n.id, n.id, n.parent_id, 0
    FROM nodes n
    WHERE n.id = ANY(@node_ids::uuid[])
      AND EXISTS (
          SELECT 1 FROM space_members sm
          WHERE sm.space_id = n.space_id AND sm.user_id = @user_id
      )
  UNION ALL
    SELECT a.origin, p.id, p.parent_id, a.depth + 1
    FROM anc a JOIN nodes p ON p.id = a.parent_id
    WHERE a.depth < 100
),
grants AS (
    SELECT anc.origin AS node_id,
           COUNT(*) > 0              AS view,
           BOOL_OR(s.can_download)   AS download,
           BOOL_OR(s.can_share)      AS share,
           BOOL_OR(s.level = 'edit') AS edit
    FROM anc
    JOIN shares s ON s.node_id = anc.id
    WHERE (s.expires_at IS NULL OR s.expires_at > now())
      AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
         OR (s.kind = 'group'  AND s.grantee_group_id IN (
                SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
    GROUP BY anc.origin
),
owners AS (
    -- Ownership is the one unconditional access path — except inside a team
    -- folder, where content belongs to the container and `created_by` is
    -- authorship only. Without this exclusion a team folder would behave
    -- exactly like a personal one: whoever uploaded a file would keep full
    -- capabilities on it after their access to the folder was revoked, could
    -- share it onward regardless of the grant, and could move it out.
    SELECT DISTINCT a.origin AS node_id
    FROM anc a JOIN nodes n2 ON n2.id = a.id
    WHERE n2.created_by = @user_id AND n2.team_root_id IS NULL
)
SELECT n.id AS node_id,
       COALESCE(g.view, false)     AS view,
       COALESCE(g.download, false) AS download,
       COALESCE(g.share, false)    AS share,
       COALESCE(g.edit, false)     AS edit,
       (o.node_id IS NOT NULL)::bool AS owner
FROM nodes n
LEFT JOIN grants g ON g.node_id = n.id
LEFT JOIN owners o ON o.node_id = n.id
WHERE n.id = ANY(@node_ids::uuid[]);

-- name: GetDirectShare :one
SELECT * FROM shares
WHERE kind = 'direct' AND node_id = $1 AND grantee_user_id = $2
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;

-- name: NodesWithLiveShares :many
-- Which of these nodes carry at least one live share row directly on them —
-- direct, group or link, regardless of grantee. This is deliberately not the
-- recursive access-caps walk: it answers "did someone explicitly share this
-- node", the fact the folder-listing badge shows, not "can the caller reach
-- it via an ancestor's grant".
SELECT DISTINCT node_id FROM shares
WHERE node_id = ANY(@node_ids::uuid[])
  AND (expires_at IS NULL OR expires_at > now());
