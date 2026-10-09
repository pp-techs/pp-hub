-- name: UpsertPageMeta :execrows
-- M43 pages. A page is a block document: a file whose current version's type
-- is text/markdown (fs.IsMarkdown, spelled in SQL as the extraction scan
-- spells it); a sub-page is a page whose parent is a page.
--
-- Store a page's icon and cover read from one version — only while that
-- version is still the node's current one, so a job that ran late for a
-- superseded version writes nothing (0 rows), as UpsertNodeText does.
INSERT INTO page_meta (node_id, node_version_id, icon, cover)
SELECT n.id, sqlc.arg('node_version_id')::uuid, sqlc.narg('icon')::text, sqlc.narg('cover')::text
FROM nodes n
WHERE n.id = sqlc.arg('node_id')::uuid
  AND n.current_version_id = sqlc.arg('node_version_id')::uuid
ON CONFLICT (node_id) DO UPDATE
SET node_version_id = EXCLUDED.node_version_id,
    icon            = EXCLUDED.icon,
    cover           = EXCLUDED.cover;

-- name: DeletePageRefsForSource :execrows
-- First half of replacing a document's references, in the same transaction
-- as InsertPageRefs: drops them only while node_version_id is the source's
-- current version. A stale job therefore leaves the newer rows alone.
DELETE FROM page_refs r
USING nodes n
WHERE r.source_id = sqlc.arg('source_id')::uuid
  AND n.id = r.source_id
  AND n.current_version_id = sqlc.arg('node_version_id')::uuid;

-- name: InsertPageRefs :execrows
-- Second half: the references one version names — what mdoc.PageRefs
-- answers, sub-page blocks and page links — under the same guard.
-- Duplicates are ignored.
INSERT INTO page_refs (source_id, target_id, kind, node_version_id)
SELECT n.id, t.target_id, t.kind, sqlc.arg('node_version_id')::uuid
FROM nodes n
CROSS JOIN (
    SELECT unnest(sqlc.arg('sub_ids')::uuid[]) AS target_id, 'sub'::text AS kind
  UNION ALL
    SELECT unnest(sqlc.arg('link_ids')::uuid[]), 'link'::text
) t
WHERE n.id = sqlc.arg('source_id')::uuid
  AND n.current_version_id = sqlc.arg('node_version_id')::uuid
ON CONFLICT (source_id, target_id, kind) DO NOTHING;

-- name: ListMarkdownWithoutPageMeta :many
-- Current versions of block documents whose page metadata has not been
-- indexed for that version and that have no indexing job in flight (@kinds).
-- Backfill only, keyset-paginated on v.id like VersionsMissingText: pass the
-- last id returned as @after (all-zero UUID to start). Trashed documents are
-- included, since restoring one from the trash commits no version.
SELECT v.id, v.node_id, v.blob_hash, v.mime, n.name
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN page_meta m ON m.node_id = n.id AND m.node_version_id = v.id
WHERE n.kind = 'file'
  AND m.node_id IS NULL
  AND btrim(split_part(lower(v.mime), ';', 1)) = 'text/markdown'
  AND v.id > sqlc.arg('after')::uuid
  AND NOT EXISTS (
      SELECT 1 FROM jobs j
      WHERE j.kind = ANY(sqlc.arg('kinds')::text[])
        AND j.payload->>'node_version_id' = v.id::text
        AND j.status IN ('pending', 'running')
  )
ORDER BY v.id
LIMIT sqlc.arg('lim');

-- name: ListPageChain :many
-- A node and every ancestor, nearest first, trashed ones included, with what
-- a page header needs: the current version's type (is it a page?), the
-- tombstone, and the icon and cover. The caller cuts the chain at the first
-- node that is not a page or that the caller cannot view.
WITH RECURSIVE chain(id, parent_id, depth) AS (
    SELECT n.id, n.parent_id, 0 FROM nodes n WHERE n.id = sqlc.arg('node_id')::uuid
  UNION ALL
    SELECT p.id, p.parent_id, c.depth + 1
    FROM chain c JOIN nodes p ON p.id = c.parent_id
    WHERE c.depth < 100
)
SELECT n.id, n.parent_id, n.kind, n.name, n.deleted_at,
       COALESCE(v.mime, '')::text AS mime,
       m.icon, m.cover
FROM chain c
JOIN nodes n ON n.id = c.id
LEFT JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN page_meta m ON m.node_id = n.id
ORDER BY c.depth;

-- name: ListChildPages :many
-- A page's live sub-pages in tree order (page_order, then created_at, then
-- name), each with whether it has live sub-pages of its own.
SELECT n.id, n.parent_id, n.name, m.icon, m.cover,
       EXISTS (
           SELECT 1 FROM nodes c
           JOIN node_versions cv ON cv.id = c.current_version_id
           WHERE c.parent_id = n.id
             AND c.deleted_at IS NULL
             AND c.kind = 'file'
             AND btrim(split_part(lower(cv.mime), ';', 1)) = 'text/markdown'
       )::bool AS has_children
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN page_meta m ON m.node_id = n.id
LEFT JOIN page_order o ON o.node_id = n.id
WHERE n.parent_id = sqlc.arg('parent_id')::uuid
  AND n.deleted_at IS NULL
  AND n.kind = 'file'
  AND btrim(split_part(lower(v.mime), ';', 1)) = 'text/markdown'
ORDER BY o.position NULLS LAST, n.created_at, n.name;

-- name: CountChildPages :many
-- How many live sub-pages each of @parent_ids holds, in one query for a
-- listing. Parents without any are absent.
SELECT n.parent_id::uuid AS parent_id, COUNT(*)::bigint AS pages
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
WHERE n.parent_id = ANY(sqlc.arg('parent_ids')::uuid[])
  AND n.deleted_at IS NULL
  AND n.kind = 'file'
  AND btrim(split_part(lower(v.mime), ';', 1)) = 'text/markdown'
GROUP BY n.parent_id;

-- name: ListBacklinks :many
-- The live pages whose references name @target_id — sub-page blocks and
-- page links alike, once each — with whether each one's parent is a page.
-- A page inside a trashed folder or document is in the trash too, so a
-- tombstone anywhere up the chain drops it. The caller filters by view.
WITH RECURSIVE src AS (
    SELECT DISTINCT r.source_id AS id
    FROM page_refs r
    WHERE r.target_id = sqlc.arg('target_id')::uuid
      AND r.source_id <> sqlc.arg('target_id')::uuid
),
up(origin, id, parent_id, trashed, depth) AS (
    SELECT n.id, n.id, n.parent_id, n.deleted_at IS NOT NULL, 0
    FROM nodes n JOIN src ON src.id = n.id
  UNION ALL
    SELECT u.origin, p.id, p.parent_id, p.deleted_at IS NOT NULL, u.depth + 1
    FROM up u JOIN nodes p ON p.id = u.parent_id
    WHERE u.depth < 100
)
SELECT n.id, n.parent_id, n.name, m.icon, m.cover,
       (p.id IS NOT NULL AND p.kind = 'file'
        AND btrim(split_part(lower(COALESCE(pv.mime, '')), ';', 1)) = 'text/markdown')::bool AS parent_is_page,
       EXISTS (
           SELECT 1 FROM nodes c
           JOIN node_versions cv ON cv.id = c.current_version_id
           WHERE c.parent_id = n.id
             AND c.deleted_at IS NULL
             AND c.kind = 'file'
             AND btrim(split_part(lower(cv.mime), ';', 1)) = 'text/markdown'
       )::bool AS has_children
FROM nodes n
JOIN src ON src.id = n.id
JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN page_meta m ON m.node_id = n.id
LEFT JOIN nodes p ON p.id = n.parent_id
LEFT JOIN node_versions pv ON pv.id = p.current_version_id
WHERE n.kind = 'file'
  AND btrim(split_part(lower(v.mime), ';', 1)) = 'text/markdown'
  AND NOT EXISTS (SELECT 1 FROM up WHERE up.origin = n.id AND up.trashed)
ORDER BY n.name, n.id;

-- name: SetPageOrder :exec
-- Writes sibling positions: @node_ids[i] gets position i.
INSERT INTO page_order (node_id, position)
SELECT t.node_id, (t.ord - 1)::int
FROM unnest(sqlc.arg('node_ids')::uuid[]) WITH ORDINALITY AS t(node_id, ord)
ON CONFLICT (node_id) DO UPDATE SET position = EXCLUDED.position;

-- name: ClearPageOrder :exec
-- Forgets a page's position, as it leaves the parent that gave it one.
DELETE FROM page_order WHERE node_id = $1;
