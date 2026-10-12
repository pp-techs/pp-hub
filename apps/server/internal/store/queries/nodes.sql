-- name: CreateNode :one
-- team_root_id is inherited from the parent, so everything created inside a
-- team folder belongs to that container without the caller having to know.
INSERT INTO nodes (space_id, parent_id, kind, name, created_by, team_root_id)
VALUES ($1, $2, $3, $4, $5,
        (SELECT p.team_root_id FROM nodes p WHERE p.id = $2))
RETURNING *;

-- name: CreateTeamRoot :one
-- A team folder: a top-level node that owns its own subtree. Its team_root_id
-- points at itself, which is what marks it as the container rather than a
-- member of one.
INSERT INTO nodes (space_id, kind, name, created_by)
VALUES ($1, 'dir', $2, $3)
RETURNING *;

-- name: MarkTeamRoot :exec
UPDATE nodes SET team_root_id = id WHERE id = $1 AND parent_id IS NULL;

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

-- name: SetSubtreeTeamRoot :exec
-- Rewrite container membership across a moved subtree. A move is the only
-- operation that changes which container a node belongs to, and it changes it
-- for everything underneath — which is why team_root_id can be denormalised
-- at all: exactly one operation has to maintain it.
--
-- @team_root_id is NULL when the subtree lands in personal space.
WITH RECURSIVE sub AS (
    SELECT n.id, 0 AS depth FROM nodes n WHERE n.id = @root_id
  UNION ALL
    SELECT c.id, s.depth + 1 FROM nodes c JOIN sub s ON c.parent_id = s.id
    WHERE s.depth < 100
)
UPDATE nodes SET team_root_id = sqlc.narg('team_root_id')::uuid
WHERE id IN (SELECT id FROM sub);

-- name: ReassignAuthor :exec
-- Give a subtree a personal owner. Used when content leaves a team folder:
-- outside a container `created_by` IS access, and a file whose author no
-- longer works here would otherwise land in personal space owned by nobody
-- reachable. Drive transfers ownership to whoever moves the item out; so do
-- we, and the version history keeps the real authors.
WITH RECURSIVE sub AS (
    SELECT n.id, 0 AS depth FROM nodes n WHERE n.id = @root_id
  UNION ALL
    SELECT c.id, s.depth + 1 FROM nodes c JOIN sub s ON c.parent_id = s.id
    WHERE s.depth < 100
)
UPDATE nodes SET created_by = @user_id
WHERE id IN (SELECT id FROM sub);

-- name: SoftDeleteNode :exec
UPDATE nodes SET deleted_at = now(), deleted_by = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetNodeParent :one
SELECT parent_id FROM nodes WHERE id = $1 AND deleted_at IS NULL LIMIT 1;

-- name: GetLiveChildByName :one
-- The live file or folder named @name directly under @parent_id (NULL: the
-- space's top level), for a document's relative reference. Names compare in
-- Unicode NFC (@name arrives in it): Finder writes names decomposed, a
-- markdown file usually spells them composed. The exact spelling wins when
-- both forms exist side by side.
SELECT * FROM nodes
WHERE space_id = @space_id
  AND parent_id IS NOT DISTINCT FROM sqlc.narg('parent_id')::uuid
  AND deleted_at IS NULL
  AND kind IN ('dir', 'file')
  AND normalize(name, NFC) = @name::text
ORDER BY (name = @name::text) DESC, created_at
LIMIT 1;

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

-- name: PruneVersions :exec
-- Deletes every version of a file beyond the newest `keep` non-current ones.
-- The current version is never a candidate — after a restore it can be an
-- old one, and it is the file. Previews go with their version (ON DELETE
-- CASCADE); the blob references of both are released by trigger.
DELETE FROM node_versions d
WHERE d.id IN (
    SELECT v.id FROM node_versions v
    JOIN nodes n ON n.id = v.node_id
    WHERE v.node_id = sqlc.arg('node_id')
      AND v.id IS DISTINCT FROM n.current_version_id
    ORDER BY v.version_no DESC
    OFFSET sqlc.arg('keep')::int
);

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
-- What a folder shows: apps' nodes live in the sidebar's Apps section, not in
-- the drive, so they do not make a folder look full.
SELECT COUNT(*) FROM nodes
WHERE parent_id = $1 AND deleted_at IS NULL AND kind <> 'ext'
LIMIT 1;

-- name: ListDeleted :many
SELECT * FROM nodes
WHERE space_id = $1 AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC;

-- name: ListChildrenWithMeta :many
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id, n.ext_kind,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
WHERE n.space_id = $1
  AND n.parent_id IS NOT DISTINCT FROM $2
  AND n.deleted_at IS NULL
  -- An app's nodes are listed under Apps in the sidebar, never among files.
  AND n.kind <> 'ext'
ORDER BY n.kind, n.name;

-- name: ListAncestry :many
-- The node and every ancestor, nearest first — the path a breadcrumb draws.
-- Callers resolve capabilities on the result and stop at the first node the
-- caller cannot open.
WITH RECURSIVE chain(id, parent_id, name, depth) AS (
    SELECT n.id, n.parent_id, n.name, 0 FROM nodes n WHERE n.id = $1
  UNION ALL
    SELECT p.id, p.parent_id, p.name, c.depth + 1
    FROM chain c JOIN nodes p ON p.id = c.parent_id
    WHERE c.depth < 100
)
SELECT id, name FROM chain ORDER BY depth;

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
-- Hard delete. Descendants, versions and previews follow via ON DELETE
-- CASCADE, and their blob references are released by trigger.
DELETE FROM nodes WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: ListDeletedIDs :many
SELECT id FROM nodes WHERE space_id = $1 AND deleted_at IS NOT NULL;

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
    WHERE a.depth < 100
),
loc AS (
    SELECT origin, string_agg(name, ' / ' ORDER BY depth DESC) AS path
    FROM anc WHERE depth > 0 GROUP BY origin
)
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id, n.ext_kind,
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
-- Name and content search across every space the caller belongs to, plus the
-- subtrees they hold an external grant on in spaces they do not, or one space
-- when space_id is supplied. Candidates are filtered by the caller's
-- capabilities afterwards (reachableRows).
--
-- Outside a space the candidates are only the granted subtrees, never the
-- whole space: the LIMIT runs before that filter, so hidden rows competing for
-- the window would let an outsider count what a company holds that matches.
--
-- A node matches on its name (ILIKE, as always) or on the text extracted from
-- its *current* version (node_texts): the folded full-text query, or the
-- folded substring when the caller asks for the fallback. A NULL
-- content_tsquery / content_like disables that branch — NULL rather than ''
-- so a generic plan never runs LIKE '%%' against the trigram index.
--
-- Every name hit ranks above every content-only hit; within names, an exact
-- prefix beats a mid-string match and then trigram similarity decides;
-- within content, ts_rank does.
WITH RECURSIVE ext(id, depth) AS (
  SELECT xs.node_id, 0 FROM shares xs
  WHERE xs.kind = 'direct' AND xs.is_external AND xs.grantee_user_id = $1
    AND (xs.expires_at IS NULL OR xs.expires_at > now())
  UNION
  SELECT c.id, e.depth + 1 FROM ext e JOIN nodes c ON c.parent_id = e.id
  WHERE e.depth < 100
),
content_hits AS (
  SELECT t.node_id, t.node_version_id,
         ts_rank(t.tsv, to_tsquery('simple', sqlc.narg('content_tsquery')::text)) AS rank
  FROM node_texts t
  WHERE t.tsv @@ to_tsquery('simple', sqlc.narg('content_tsquery')::text)
     OR t.folded LIKE '%' || sqlc.narg('content_like')::text || '%'
)
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id, n.ext_kind,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  s.name AS space_name,
  COALESCE(p.name, '')::text AS parent_name,
  (n.name ILIKE '%' || sqlc.arg('query')::text || '%')::bool AS name_match
FROM nodes n
JOIN spaces s ON s.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN nodes p ON p.id = n.parent_id AND p.deleted_at IS NULL
LEFT JOIN content_hits h ON h.node_id = n.id AND h.node_version_id = n.current_version_id
WHERE n.deleted_at IS NULL
  AND (EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = n.space_id AND m.user_id = $1)
       OR n.id IN (SELECT ext.id FROM ext))
  AND (sqlc.narg('space_id')::uuid IS NULL OR n.space_id = sqlc.narg('space_id')::uuid)
  AND (sqlc.narg('kind')::node_kind IS NULL OR n.kind = sqlc.narg('kind')::node_kind)
  AND (sqlc.narg('owner_id')::uuid IS NULL OR n.created_by = sqlc.narg('owner_id')::uuid)
  -- Modified time is the current version's timestamp for a file and the
  -- node's own for a folder, so the filter has to consider both.
  AND (sqlc.narg('modified_after')::timestamptz IS NULL
       OR COALESCE(v.created_at, n.created_at) >= sqlc.narg('modified_after')::timestamptz)
  AND (n.name ILIKE '%' || sqlc.arg('query')::text || '%' OR h.node_id IS NOT NULL)
ORDER BY
  (n.name ILIKE '%' || sqlc.arg('query')::text || '%') DESC,
  (lower(n.name) LIKE lower(sqlc.arg('query')::text) || '%') DESC,
  (CASE WHEN n.name ILIKE '%' || sqlc.arg('query')::text || '%'
        THEN similarity(n.name, sqlc.arg('query')::text) ELSE 0 END) DESC,
  COALESCE(h.rank, 0) DESC,
  n.kind,
  n.name
LIMIT sqlc.arg('result_limit');

-- name: StarNode :exec
INSERT INTO node_stars (user_id, node_id) VALUES ($1, $2)
ON CONFLICT (user_id, node_id) DO NOTHING;

-- name: UnstarNode :exec
DELETE FROM node_stars WHERE user_id = $1 AND node_id = $2;

-- name: ListStarredNodes :many
-- The caller's starred nodes, newest star first. Reach is rechecked here —
-- membership, or an external grant in the space — because a star survives
-- losing access; callers filter by capabilities afterwards. A null space_id
-- spans every space; a set one scopes the listing to it.
SELECT
  n.id, n.tenant_id, n.space_id, n.parent_id, n.kind, n.name,
  n.current_version_id, n.deleted_at, n.created_by, n.created_at, n.team_root_id, n.ext_kind,
  v.id AS v_id, v.blob_hash, v.size AS v_size, v.mime, v.version_no, v.created_by AS v_created_by, v.created_at AS v_created_at,
  u.id AS owner_uid, u.name AS owner_name, u.email AS owner_email,
  s.name AS space_name,
  COALESCE(p.name, '')::text AS parent_name
FROM node_stars st
JOIN nodes n ON n.id = st.node_id
JOIN spaces s ON s.id = n.space_id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN users u ON u.id = n.created_by
LEFT JOIN nodes p ON p.id = n.parent_id AND p.deleted_at IS NULL
WHERE st.user_id = $1 AND n.deleted_at IS NULL
  AND (EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = n.space_id AND m.user_id = st.user_id)
       OR EXISTS (
           SELECT 1 FROM shares xs JOIN nodes xn ON xn.id = xs.node_id
           WHERE xn.space_id = n.space_id AND xs.kind = 'direct' AND xs.is_external
             AND xs.grantee_user_id = st.user_id))
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
--
-- Team content is skipped: `created_by` there grants nothing, so there is
-- nothing to hand over, and rewriting it would erase the only record of who
-- wrote what.
WITH RECURSIVE up(origin, id, parent_id, created_by, depth) AS (
    SELECT n.id, n.id, n.parent_id, n.created_by, 0
    FROM nodes n
    WHERE n.space_id = @space_id AND n.created_by = @user_id
      AND n.team_root_id IS NULL
  UNION ALL
    SELECT u.origin, p.id, p.parent_id, p.created_by, u.depth + 1
    FROM up u JOIN nodes p ON p.id = u.parent_id
    WHERE u.created_by = @user_id AND u.depth < 100
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
--
-- The Archive itself is excluded. The space creator owns it, so without this
-- their departure re-parented the Archive under the Archive: a cycle that
-- hangs every ancestor walk in the space.
--
-- Team folders are excluded for a different reason: whoever created one does
-- not own it, the team does. Archiving it on their departure would take the
-- company's shared content with one person leaving.
SELECT n.id FROM nodes n
JOIN spaces s ON s.id = n.space_id
WHERE n.space_id = $1 AND n.created_by = $2 AND n.parent_id IS NULL
  AND n.id IS DISTINCT FROM s.archive_root_id
  AND n.team_root_id IS NULL;

-- name: MoveUnderArchiveRoot :exec
-- Reparent without the live-only filter MoveNode has: a departed member's
-- trashed roots move too, so no node in the space keeps a gone owner.
UPDATE nodes SET parent_id = $2 WHERE id = $1;

-- name: CountForeignOwnedInSubtree :one
-- How many live nodes under @root_id were created by somebody other than
-- @user_id. Drives the "this folder holds N items other people made" warning
-- a destructive action shows before it runs.
WITH RECURSIVE sub AS (
    SELECT n.id, n.created_by, 0 AS depth
    FROM nodes n
    WHERE n.parent_id = @root_id AND n.deleted_at IS NULL
  UNION ALL
    SELECT c.id, c.created_by, s.depth + 1
    FROM nodes c JOIN sub s ON c.parent_id = s.id
    WHERE s.depth < 100 AND c.deleted_at IS NULL
)
SELECT COUNT(*)::bigint AS foreign_count
FROM nodes x
WHERE x.id IN (SELECT id FROM sub) AND x.created_by <> @user_id;

-- name: ListForeignTopmostInSubtree :many
-- The topmost nodes under @root_id that another member of the space created.
-- The walk descends through nodes @user_id owns, so each returned row is the
-- head of a subtree belonging to somebody else — move it and its contents
-- follow.
--
-- Content by somebody who never belonged to the space — an outsider who added
-- it through an external edit grant — is not rescued: lifted to the top level
-- it would sit outside that grant, owned by nobody who can reach it. It
-- follows the purge like the purger's own, and the walk descends through it
-- so a member's work beneath is still found. A departed member awaiting
-- handover still counts as belonging: the handover files their top-level work
-- in the Archive.
--
-- Tombstones are included on purpose: a purge would take them with it, and a
-- colleague's trashed file is still their file.
WITH RECURSIVE sub AS (
    SELECT n.id, n.created_by, n.space_id, 0 AS depth
    FROM nodes n
    WHERE n.parent_id = @root_id
  UNION ALL
    SELECT c.id, c.created_by, c.space_id, s.depth + 1
    FROM nodes c JOIN sub s ON c.parent_id = s.id
    WHERE s.depth < 100
      AND (s.created_by = @user_id OR NOT (
              EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = s.space_id AND m.user_id = s.created_by)
           OR EXISTS (SELECT 1 FROM space_departures d
                      WHERE d.space_id = s.space_id AND d.user_id = s.created_by AND d.resolved_at IS NULL)))
)
SELECT x.id, x.created_by
FROM nodes x
WHERE x.id IN (SELECT id FROM sub) AND x.created_by <> @user_id
  AND (EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = x.space_id AND m.user_id = x.created_by)
    OR EXISTS (SELECT 1 FROM space_departures d
               WHERE d.space_id = x.space_id AND d.user_id = x.created_by AND d.resolved_at IS NULL));

-- name: DetachNodeToTopLevel :exec
-- Lift a node out of whatever folder it sits in, keeping its deleted_at.
-- Used when a purge would otherwise destroy content the purger does not own:
-- the node lands at its owner's top level instead of following the cascade.
UPDATE nodes SET parent_id = NULL, name = $2 WHERE id = $1;

-- name: ListTrashRootsWithUserContent :many
-- Tombstones in @space_id that have a live descendant created by @user_id,
-- with how many such nodes are buried under each.
--
-- The climb goes upward from the caller's own nodes and stops at the first
-- tombstoned ancestor, so each node is attributed to the tombstone that
-- actually hid it rather than to every trashed folder above it.
WITH RECURSIVE up AS (
    SELECT n.id AS origin, n.parent_id AS id, 0 AS depth
    FROM nodes n
    WHERE n.space_id = @space_id AND n.created_by = @user_id
      AND n.deleted_at IS NULL AND n.parent_id IS NOT NULL
  UNION ALL
    SELECT u.origin, p.parent_id, u.depth + 1
    FROM up u JOIN nodes p ON p.id = u.id
    WHERE u.depth < 100 AND p.deleted_at IS NULL AND p.parent_id IS NOT NULL
)
SELECT t.id AS tombstone_id, COUNT(DISTINCT u.origin)::bigint AS own_count
FROM up u JOIN nodes t ON t.id = u.id
WHERE t.deleted_at IS NOT NULL
GROUP BY t.id;

-- name: CountOwnedInSubtree :one
-- How many live nodes under @root_id @user_id created. Revoking a grant does
-- not touch them — ownership is intrinsic — so the share dialog says how much
-- the grantee keeps before the revoke goes through.
WITH RECURSIVE sub AS (
    SELECT n.id, 0 AS depth
    FROM nodes n
    WHERE n.parent_id = @root_id AND n.deleted_at IS NULL
  UNION ALL
    SELECT c.id, s.depth + 1
    FROM nodes c JOIN sub s ON c.parent_id = s.id
    WHERE s.depth < 100 AND c.deleted_at IS NULL
)
SELECT COUNT(*)::bigint AS owned_count
FROM nodes x
WHERE x.id IN (SELECT id FROM sub) AND x.created_by = @user_id;

-- name: ListChainPrincipals :many
-- Everyone who reaches @root_id by inheritance: the creator of the node and of
-- every ancestor (ownership is intrinsic and travels down), plus every live
-- grant anywhere on that chain — a named person, a group, or a link.
--
-- Moving a node changes which chain it hangs from, so comparing this set
-- before and after is the honest answer to "who gains and who loses access",
-- which is the warning a move has to show. Drive shows the same thing.
WITH RECURSIVE chain AS (
    SELECT n.id, n.parent_id, n.created_by, n.team_root_id, 0 AS depth
    FROM nodes n WHERE n.id = @root_id
  UNION ALL
    SELECT p.id, p.parent_id, p.created_by, p.team_root_id, c.depth + 1
    FROM chain c JOIN nodes p ON p.id = c.parent_id
    WHERE c.depth < 100
)
-- Authors count only outside a team folder, where `created_by` is access. In
-- one, it is a byline: whoever uploaded a file there reaches it through the
-- grant on the folder like everybody else, so listing them as gaining or
-- losing access on a move would be wrong.
SELECT 'user'::text AS kind,
       u.id AS principal_id,
       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.name, ''), u.email)::text AS name
FROM chain c JOIN users u ON u.id = c.created_by
WHERE c.team_root_id IS NULL
UNION
SELECT CASE s.kind WHEN 'group' THEN 'group' WHEN 'link' THEN 'link' ELSE 'user' END::text,
       COALESCE(s.grantee_user_id, s.grantee_group_id, s.id),
       COALESCE(g.name,
                NULLIF(gu.display_name, ''), NULLIF(gu.name, ''), gu.email,
                'Anyone with the link')::text
FROM chain c
JOIN shares s ON s.node_id = c.id
LEFT JOIN groups g ON g.id = s.grantee_group_id
LEFT JOIN users gu ON gu.id = s.grantee_user_id
WHERE s.expires_at IS NULL OR s.expires_at > now();

-- name: ListInheritedAccess :many
-- Who reaches @node_id from ABOVE it, with what, and through which folder —
-- the half of "who can open this" the node's own share rows do not show.
-- Owners are the creator of the node itself (depth 0) and of every ancestor,
-- outside a team folder only; grants are those on a strict ancestor, since
-- grants on the node itself are listed by ListSharesForNode/ListGrantsForNode.
-- A principal can appear more than once (several folders above); the caller
-- keeps the nearest.
WITH RECURSIVE chain AS (
    SELECT n.id, n.parent_id, n.created_by, n.team_root_id, n.name, 0 AS depth
    FROM nodes n WHERE n.id = @node_id
  UNION ALL
    SELECT p.id, p.parent_id, p.created_by, p.team_root_id, p.name, c.depth + 1
    FROM chain c JOIN nodes p ON p.id = c.parent_id
    WHERE c.depth < 100
)
SELECT 'owner'::text AS kind,
       u.id AS principal_id,
       COALESCE(NULLIF(u.display_name, ''), NULLIF(u.name, ''), u.email)::text AS name,
       c.id AS via_id, c.name::text AS via_name, c.depth::int AS depth,
       'edit'::share_level AS level, true AS can_download, true AS can_share
FROM chain c JOIN users u ON u.id = c.created_by
WHERE c.team_root_id IS NULL
UNION ALL
SELECT CASE s.kind WHEN 'group' THEN 'group' WHEN 'link' THEN 'link' ELSE 'user' END::text,
       COALESCE(s.grantee_user_id, s.grantee_group_id, s.id),
       COALESCE(g.name,
                NULLIF(gu.display_name, ''), NULLIF(gu.name, ''), gu.email,
                'Anyone with the link')::text,
       c.id, c.name::text, c.depth::int,
       s.level, s.can_download, s.can_share
FROM chain c
JOIN shares s ON s.node_id = c.id
LEFT JOIN groups g ON g.id = s.grantee_group_id
LEFT JOIN users gu ON gu.id = s.grantee_user_id
WHERE c.depth > 0 AND (s.expires_at IS NULL OR s.expires_at > now())
ORDER BY depth, kind, name;


-- name: ListTeamRoots :many
-- The team folders of a space: nodes that are their own container.
SELECT id FROM nodes
WHERE space_id = $1 AND team_root_id = id AND deleted_at IS NULL
ORDER BY name;

-- name: CountTeamManagers :one
-- Live grants on $1 that carry can_share — the managers of a team folder.
-- A team folder with none is unadministrable: nobody can add a member, and
-- the content belongs to a container no person owns.
SELECT COUNT(*)::bigint AS managers FROM shares
WHERE node_id = $1 AND can_share AND (expires_at IS NULL OR expires_at > now());