-- name: ListTreeChildren :many
-- A folder's or page's live children as an export walks them (fs.Tree): each
-- with its current version's type, so the walk knows a page (a block
-- document) from any other file, and whether it holds live sub-pages, so a
-- page without any costs no further query. Order as ListChildren.
SELECT sqlc.embed(n),
       COALESCE(v.mime, '')::text AS mime,
       (n.kind = 'file' AND EXISTS (
           SELECT 1 FROM nodes c
           JOIN node_versions cv ON cv.id = c.current_version_id
           WHERE c.parent_id = n.id
             AND c.deleted_at IS NULL
             AND c.kind = 'file'
             AND btrim(split_part(lower(cv.mime), ';', 1)) = 'text/markdown'
       ))::bool AS has_pages
FROM nodes n
LEFT JOIN node_versions v ON v.id = n.current_version_id
WHERE n.space_id = @space_id
  AND n.parent_id = @parent_id
  AND n.deleted_at IS NULL
ORDER BY n.kind, n.name;
