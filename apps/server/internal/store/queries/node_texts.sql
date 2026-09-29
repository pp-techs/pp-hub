-- name: UpsertNodeText :execrows
-- Store the text extracted from a file version — only while that version is
-- still the node's current one. A job that ran late for a superseded version
-- therefore writes nothing (0 rows) instead of replacing the newer text; the
-- newer version has its own job.
INSERT INTO node_texts (node_id, node_version_id, content, folded)
SELECT n.id, sqlc.arg('node_version_id')::uuid, sqlc.arg('content')::text, sqlc.arg('folded')::text
FROM nodes n
WHERE n.id = sqlc.arg('node_id')::uuid
  AND n.current_version_id = sqlc.arg('node_version_id')::uuid
ON CONFLICT (node_id) DO UPDATE
SET node_version_id = EXCLUDED.node_version_id,
    content         = EXCLUDED.content,
    folded          = EXCLUDED.folded,
    extracted_at    = now();

-- name: VersionsMissingText :many
-- Current versions of files whose text has not been extracted for that
-- version and that have no extraction job in flight. Backfill only — the
-- server runs it at startup and cmd/worker --backfill runs it on demand.
--
-- Unlike the preview scan this includes trashed files: restoring one from the
-- trash does not commit a version, so nothing else would ever index it.
--
-- Keyset-paginated on v.id; pass the last id returned as @after (all-zero UUID
-- to start).
SELECT v.id, v.node_id, v.blob_hash, v.mime, n.name
FROM nodes n
JOIN node_versions v ON v.id = n.current_version_id
LEFT JOIN node_texts t ON t.node_id = n.id AND t.node_version_id = v.id
WHERE n.kind = 'file'
  AND t.node_id IS NULL
  AND btrim(split_part(lower(v.mime), ';', 1)) = ANY(sqlc.arg('mimes')::text[])
  AND v.id > sqlc.arg('after')::uuid
  AND NOT EXISTS (
      SELECT 1 FROM jobs j
      WHERE j.kind = ANY(sqlc.arg('kinds')::text[])
        AND j.payload->>'node_version_id' = v.id::text
        AND j.status IN ('pending', 'running')
  )
ORDER BY v.id
LIMIT sqlc.arg('lim');

-- name: NodeTextExcerpts :many
-- A window of each node's current text around its first hit, for search
-- snippets. Only the window leaves the database: a result page would
-- otherwise ship every matching document whole on each keystroke.
--
-- The hit is the earliest term that starts a word (the full-text query is a
-- per-term prefix match), else the first occurrence of the whole phrase (the
-- substring fallback). `folded` is character-aligned with `content`, so a
-- position found in one is valid in the other. excerpt_start is 1-based.
SELECT t.node_id,
       x.start::int AS excerpt_start,
       substr(t.content, x.start, sqlc.arg('excerpt_len')::int)::text AS excerpt,
       (x.start - 1 + sqlc.arg('excerpt_len')::int < char_length(t.content))::bool AS more_after
FROM node_texts t
JOIN nodes n ON n.id = t.node_id AND n.current_version_id = t.node_version_id
CROSS JOIN LATERAL (
    SELECT greatest(1, COALESCE(
        (SELECT min(NULLIF(regexp_instr(t.folded, '(^|[^[:alnum:]])' || term), 0))
         FROM unnest(sqlc.arg('terms')::text[]) AS term),
        NULLIF(strpos(t.folded, sqlc.arg('phrase')::text), 0),
        1) - sqlc.arg('context_before')::int) AS start
) x
WHERE t.node_id = ANY(sqlc.arg('node_ids')::uuid[]);
