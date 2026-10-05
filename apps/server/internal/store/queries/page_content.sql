-- name: ListPageTitles :many
-- The names of the live files among @ids in one space: what a document's
-- sub-page blocks and page links render as their titles (M43). Ids that are
-- trashed, missing or in another space are absent, and render as Untitled.
SELECT n.id, n.name
FROM nodes n
WHERE n.id = ANY(sqlc.arg('ids')::uuid[])
  AND n.space_id = sqlc.arg('space_id')::uuid
  AND n.deleted_at IS NULL
  AND n.kind = 'file';
