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

-- name: NodeAccessRanks :many
-- The highest permission $2 holds on each node in $1, granted either directly
-- to them or to a group they belong to, on the node itself or on any ancestor.
-- Nodes with no grant are absent from the result rather than returned as 0.
--
-- This replaces an ancestor walk that issued one GetNodeParent round-trip per
-- level, per node, and silently stopped widening at depth 100. Permission is
-- now resolved for a whole listing in one query.
--
-- The recursive join deliberately does NOT filter deleted_at. GetNodeParent
-- does, which truncates the chain at the first trashed ancestor and loses the
-- grant above it — the reason trash could not use share resolution at all.
-- ListDeletedWithMeta walks the same way for the same reason.
WITH RECURSIVE anc(origin, id, parent_id) AS (
    SELECT n.id, n.id, n.parent_id FROM nodes n WHERE n.id = ANY(@node_ids::uuid[])
  UNION ALL
    SELECT a.origin, p.id, p.parent_id FROM anc a JOIN nodes p ON p.id = a.parent_id
)
SELECT anc.origin AS node_id,
       MAX(CASE s.perm WHEN 'edit' THEN 3 WHEN 'download' THEN 2 WHEN 'view' THEN 1 ELSE 0 END)::int AS rank
FROM anc
JOIN shares s ON s.node_id = anc.id
WHERE (s.expires_at IS NULL OR s.expires_at > now())
  AND ( (s.kind = 'direct' AND s.grantee_user_id = @user_id)
     OR (s.kind = 'group'  AND s.grantee_group_id IN (
            SELECT gm.group_id FROM group_members gm WHERE gm.user_id = @user_id)) )
GROUP BY anc.origin;

-- name: GetDirectShare :one
SELECT * FROM shares
WHERE kind = 'direct' AND node_id = $1 AND grantee_user_id = $2
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1;
