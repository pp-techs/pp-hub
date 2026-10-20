-- Publishing (M50). publication_files rows are blob references and count
-- toward their space's used_bytes through triggers (0030); nothing here
-- moves either figure by hand.

-- name: CreatePublication :one
INSERT INTO publications (space_id, node_id, title, entry_path, token_hash, token_sealed,
                          password_hash, expires_at, created_by)
VALUES (@space_id, @node_id, @title, @entry_path, @token_hash, @token_sealed,
        sqlc.narg('password_hash'), sqlc.narg('expires_at'), @created_by)
RETURNING *;

-- name: GetPublicationByNode :one
SELECT * FROM publications WHERE node_id = $1 LIMIT 1;

-- name: LockPublicationByNode :one
-- The row a republish replaces the files of, held until it commits so two
-- republishes cannot interleave their manifests.
SELECT * FROM publications WHERE node_id = $1 LIMIT 1 FOR UPDATE;

-- name: GetServedPublication :one
-- A publication by its token, with every fact a request re-checks before it
-- serves anything: the space's switch and state, and whether the creator
-- still belongs to the space — and the creator's name for the viewer's bar.
SELECT sqlc.embed(p),
       s.allow_public_links,
       (s.archived_at IS NOT NULL)::bool AS space_archived,
       EXISTS (SELECT 1 FROM space_members m
                WHERE m.space_id = p.space_id AND m.user_id = p.created_by)::bool AS creator_is_member,
       COALESCE(NULLIF(u.display_name, ''), u.name)::text AS creator_name
FROM publications p
JOIN spaces s ON s.id = p.space_id
JOIN users u ON u.id = p.created_by
WHERE p.token_hash = $1
LIMIT 1;

-- name: RepublishPublication :one
UPDATE publications
   SET title = @title, entry_path = @entry_path, rev = rev + 1, published_at = now()
 WHERE id = @id
RETURNING *;

-- name: UpdatePublicationSettings :one
UPDATE publications
   SET password_hash = sqlc.narg('password_hash'), expires_at = sqlc.narg('expires_at')
 WHERE id = @id
RETURNING *;

-- name: DeletePublication :execrows
DELETE FROM publications WHERE id = $1;

-- name: DeleteExpiredPublications :execrows
-- The five-minute sweep. A request already refuses an expired row; deleting
-- it releases its blobs and its share of the space's used bytes.
DELETE FROM publications WHERE expires_at IS NOT NULL AND expires_at <= now();

-- name: DeletePublicationFiles :exec
DELETE FROM publication_files WHERE publication_id = $1;

-- name: InsertPublicationFiles :exec
-- The whole manifest in one statement; the arrays are parallel (set-returning
-- functions in one select list advance together).
INSERT INTO publication_files (publication_id, path, blob_hash, size, mime)
SELECT @publication_id::uuid, unnest(@paths::text[]), unnest(@blob_hashes::text[]),
       unnest(@sizes::bigint[]), unnest(@mimes::text[]);

-- name: GetPublicationFile :one
SELECT * FROM publication_files WHERE publication_id = $1 AND path = $2 LIMIT 1;

-- name: ListPublicationFiles :many
SELECT * FROM publication_files WHERE publication_id = $1 ORDER BY path;

-- name: PublicationTotals :one
SELECT count(*)::bigint AS file_count, COALESCE(sum(size), 0)::bigint AS total_bytes
FROM publication_files WHERE publication_id = $1;

-- name: ListPublishableFiles :many
-- Every live file at any depth under a folder, with its path relative to
-- the folder and its current version: what a snapshot takes. Only folders
-- are descended — a document's sub-pages are not part of the site — and
-- nodes with no bytes (extension nodes) never match kind = 'file'. The
-- depth bound matches fs's maxTreeDepth.
WITH RECURSIVE tree AS (
    SELECT n.id, n.kind, n.name::text AS path, n.current_version_id, 1 AS depth
      FROM nodes n
     WHERE n.parent_id = @root_id AND n.deleted_at IS NULL
    UNION ALL
    SELECT c.id, c.kind, t.path || '/' || c.name, c.current_version_id, t.depth + 1
      FROM nodes c
      JOIN tree t ON c.parent_id = t.id
     WHERE t.kind = 'dir' AND c.deleted_at IS NULL AND t.depth < 100
)
SELECT t.path::text AS path, v.blob_hash, v.size, v.mime, v.created_at
FROM tree t
JOIN node_versions v ON v.id = t.current_version_id
WHERE t.kind = 'file'
ORDER BY t.path;
