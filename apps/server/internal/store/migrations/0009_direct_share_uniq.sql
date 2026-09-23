-- One direct grant per (node, person), the same rule shares_group_uniq already
-- holds for groups. Without it re-sharing stacked a second row beside the
-- first, and since resolution ORs every row together the wider one always
-- won: narrowing somebody's access meant finding and revoking every row that
-- named them. Re-sharing is now an upsert (UpsertDirectShare).
--
-- Existing duplicates are folded first. Expired rows grant nothing, so a
-- duplicate that has lapsed is dropped rather than merged — merging it would
-- revive whatever it once granted. What is left is merged into the oldest
-- row: level and flags take the widest, expiry the latest, and no expiry
-- wins over any date, which is exactly what resolution already answered.
DELETE FROM shares s
 WHERE s.kind = 'direct'
   AND s.expires_at IS NOT NULL AND s.expires_at <= now()
   AND EXISTS (
       SELECT 1 FROM shares o
        WHERE o.kind = 'direct' AND o.id <> s.id
          AND o.node_id = s.node_id AND o.grantee_user_id = s.grantee_user_id);

WITH dup AS (
    SELECT node_id, grantee_user_id,
           (array_agg(id ORDER BY created_at, id))[1]                   AS keep_id,
           bool_or(level = 'edit')                                       AS edit,
           bool_or(can_download)                                         AS dl,
           bool_or(can_share)                                            AS sh,
           CASE WHEN bool_or(expires_at IS NULL) THEN NULL ELSE max(expires_at) END AS exp
      FROM shares
     WHERE kind = 'direct'
     GROUP BY node_id, grantee_user_id
    HAVING count(*) > 1
), merged AS (
    UPDATE shares s
       SET level        = CASE WHEN d.edit THEN 'edit'::share_level ELSE 'view'::share_level END,
           can_download = d.dl OR d.edit,
           can_share    = d.sh,
           expires_at   = d.exp
      FROM dup d
     WHERE s.id = d.keep_id
    RETURNING s.id
)
DELETE FROM shares s
 USING dup d
 WHERE s.kind = 'direct'
   AND s.node_id = d.node_id AND s.grantee_user_id = d.grantee_user_id
   AND s.id <> d.keep_id;

CREATE UNIQUE INDEX shares_direct_uniq ON shares (node_id, grantee_user_id) WHERE kind = 'direct';
