-- name: CreateShare :one
INSERT INTO shares (node_id, kind, token_hash, grantee_user_id, level, can_download, can_share, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpsertDirectShare :one
-- shares_direct_uniq makes re-sharing with the same person an update rather
-- than a second row that resolution would OR with the first — which made
-- narrowing someone's access impossible without revoking every row.
--
-- is_external is decided by the caller at grant time — whether the grantee
-- is outside the node's space — and rewritten on every re-share, so a grant
-- made while someone was an outsider becomes an ordinary one once they join.
INSERT INTO shares (node_id, kind, grantee_user_id, level, can_download, can_share, expires_at, created_by, is_external)
VALUES (@node_id, 'direct', @grantee_user_id, @level, @can_download, @can_share, @expires_at, @created_by, @is_external)
ON CONFLICT (node_id, grantee_user_id) WHERE kind = 'direct'
DO UPDATE SET level = EXCLUDED.level, can_download = EXCLUDED.can_download,
              can_share = EXCLUDED.can_share, expires_at = EXCLUDED.expires_at,
              created_by = EXCLUDED.created_by, is_external = EXCLUDED.is_external
RETURNING *;

-- name: GetShareOnNode :one
-- One share row, only if it sits on the node the request authorised against.
SELECT * FROM shares WHERE id = $1 AND node_id = $2 LIMIT 1;

-- name: GetShareByToken :one
SELECT * FROM shares
WHERE token_hash = $1
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;

-- name: ListSharesForNode :many
SELECT * FROM shares WHERE node_id = $1 ORDER BY created_at DESC;

-- The node is part of the predicate, not just the authorization check at the
-- edge: `CapShare` is resolved against {nodeID} in the URL, so without this a
-- caller who may share one node could delete a share row belonging to another
-- by naming its id.
-- name: DeleteShare :execrows
DELETE FROM shares WHERE id = $1 AND node_id = $2;

-- name: NodeAccessCaps :many
-- For each node in $1: the capabilities $2 holds via grants — direct or via a
-- group — on the node itself or any ancestor, plus whether $2 OWNS the node
-- or any ancestor (created_by). Ownership is intrinsic full access, so the
-- owner flag must survive the no-grant case: one row per requested node.
--
-- There is no role floor anymore (M15): an admin who owns nothing and was
-- granted nothing resolves to no capabilities, like anyone else.
--
-- Membership of the node's space decides which paths count. Ownership is
-- intrinsic but not perpetual: leaving a company ends the caller's reach into
-- its tree, and their `created_by` rows stay only so the archive keeps an
-- author. Without this, RemoveMember would file someone's work in the archive
-- while leaving them able to read and edit it.
--
-- A non-member reaches a node through exactly one path: a direct share marked
-- `is_external` on it or an ancestor, and only while the space allows
-- external sharing, the instance does (@external_enabled), the space is not
-- archived, and whoever made the share is still a member of the space — a
-- departed member's outside grants stop at their departure, not at the
-- handover an admin may never confirm. No group grant, no ownership. A
-- non-member whose space allows nothing is not seeded at all, so the walk
-- costs them nothing.
--
-- The recursive join deliberately does NOT filter deleted_at. GetNodeParent
-- does, which truncates the chain at the first trashed ancestor and loses the
-- grant above it — the reason trash resolves exactly like the live tree.
-- ListDeletedWithMeta walks the same way for the same reason.
--
-- UNION ALL cannot dedup its way out of a parent cycle, and this query gates
-- every node-addressed route: one cycle would hang the whole space. The depth
-- cap is the brake; nodes_no_self_parent_ck keeps the obvious cycle out.
WITH RECURSIVE seed AS (
    SELECT n.id, n.parent_id, n.space_id,
           EXISTS (
               SELECT 1 FROM space_members sm
               WHERE sm.space_id = n.space_id AND sm.user_id = @user_id
           ) AS member,
           (sp.external_sharing AND sp.archived_at IS NULL AND @external_enabled::bool) AS external_ok
    FROM nodes n
    JOIN spaces sp ON sp.id = n.space_id
    WHERE n.id = ANY(@node_ids::uuid[])
),
anc(origin, id, parent_id, space_id, member, depth) AS (
    SELECT seed.id, seed.id, seed.parent_id, seed.space_id, seed.member, 0
    FROM seed
    WHERE seed.member OR seed.external_ok
  UNION ALL
    SELECT a.origin, p.id, p.parent_id, a.space_id, a.member, a.depth + 1
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
      AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id
             AND (anc.member OR (s.is_external AND EXISTS (
                     SELECT 1 FROM space_members cm
                     WHERE cm.space_id = anc.space_id AND cm.user_id = s.created_by))))
         OR (anc.member AND s.kind = 'group' AND s.grantee_group_id IN (
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
    WHERE a.member AND n2.created_by = @user_id AND n2.team_root_id IS NULL
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
