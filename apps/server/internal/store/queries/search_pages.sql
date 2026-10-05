-- name: ListPageLocations :many
-- M43: where a search hit lives when its parent is a page. For each of
-- @ids (the hits' parents), that node and its ancestors nearest first,
-- climbing only while the node just reached is a page (a markdown file), so
-- each chain ends with the folder holding the topmost page — or with the top
-- page when it sits at the space root. A parent that is not a page answers
-- itself alone. The caller cuts each chain at the first node it cannot view.
WITH RECURSIVE up(origin, id, parent_id, name, is_page, depth) AS (
    SELECT n.id, n.id, n.parent_id, n.name,
           (n.kind = 'file'
            AND btrim(split_part(lower(COALESCE(v.mime, '')), ';', 1)) = 'text/markdown')::bool,
           0
    FROM nodes n
    LEFT JOIN node_versions v ON v.id = n.current_version_id
    WHERE n.id = ANY(sqlc.arg('ids')::uuid[])
  UNION ALL
    SELECT u.origin, p.id, p.parent_id, p.name,
           (p.kind = 'file'
            AND btrim(split_part(lower(COALESCE(pv.mime, '')), ';', 1)) = 'text/markdown')::bool,
           u.depth + 1
    FROM up u
    JOIN nodes p ON p.id = u.parent_id
    LEFT JOIN node_versions pv ON pv.id = p.current_version_id
    WHERE u.is_page AND u.depth < 100
)
SELECT origin::uuid AS origin, id::uuid AS id, name::text AS name, is_page::bool AS is_page
FROM up
ORDER BY origin, depth;
