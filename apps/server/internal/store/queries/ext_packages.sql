-- Packaged apps (M51): the installed packages, their files and the record
-- store of their nodes.

-- name: ListExtPackages :many
SELECT ext_id, manifest, version, digest, size, backend_url, backend_secret,
       installed_by, installed_at, updated_by, updated_at
FROM ext_packages
ORDER BY ext_id;

-- name: GetExtPackage :one
SELECT ext_id, manifest, version, digest, size, backend_url, backend_secret,
       installed_by, installed_at, updated_by, updated_at
FROM ext_packages
WHERE ext_id = $1;

-- name: UpsertExtPackage :one
-- Install or upgrade. An upgrade keeps the administrator's backend address
-- and secret, and who installed it first.
INSERT INTO ext_packages (ext_id, manifest, version, digest, size, installed_by, updated_by)
VALUES (@ext_id, @manifest, @version, @digest, @size, @by, @by)
ON CONFLICT (ext_id) DO UPDATE
SET manifest = EXCLUDED.manifest, version = EXCLUDED.version, digest = EXCLUDED.digest,
    size = EXCLUDED.size, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING ext_id, manifest, version, digest, size, backend_url, backend_secret,
          installed_by, installed_at, updated_by, updated_at;

-- name: DeleteExtPackageFiles :exec
DELETE FROM ext_package_files WHERE ext_id = $1;

-- name: InsertExtPackageFiles :copyfrom
INSERT INTO ext_package_files (ext_id, path, content_type, sha256, body)
VALUES ($1, $2, $3, $4, $5);

-- name: GetExtPackageFile :one
-- One asset of an installed package, by the digest the URL names: a URL of a
-- replaced version answers nothing rather than the new version's bytes.
SELECT f.content_type, f.sha256, f.body
FROM ext_package_files f
JOIN ext_packages p ON p.ext_id = f.ext_id
WHERE f.ext_id = @ext_id AND p.digest = @digest AND f.path = @path;

-- name: DeleteExtPackage :execrows
DELETE FROM ext_packages WHERE ext_id = $1;

-- name: SetExtPackageBackendURL :execrows
UPDATE ext_packages SET backend_url = sqlc.narg(backend_url), updated_by = @by, updated_at = now()
WHERE ext_id = @ext_id;

-- name: SetExtPackageBackendSecret :execrows
UPDATE ext_packages SET backend_secret = @backend_secret, updated_by = @by, updated_at = now()
WHERE ext_id = @ext_id;

-- name: DeleteExtStateForExt :exec
-- Removing an app forgets its switches, so a reinstall starts off.
DELETE FROM ext_state WHERE ext_id = $1;

-- name: PurgeExtRecordsForExt :execrows
-- Removing an app with its data: every record of every node of its kinds.
-- The nodes stay, as items of an app this installation no longer carries.
DELETE FROM ext_records r
USING nodes n
WHERE r.node_id = n.id AND n.kind = 'ext' AND n.ext_kind LIKE @ext_prefix::text;

-- name: ListExtRecords :many
-- A page of a node's records in creation order, optionally of one collection,
-- after a (created_at, id) cursor.
SELECT node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at
FROM ext_records
WHERE node_id = @node_id
  AND (sqlc.narg(collection)::text IS NULL OR collection = sqlc.narg(collection)::text)
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (created_at, id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT @max_rows;

-- name: GetExtRecord :one
SELECT node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at
FROM ext_records
WHERE node_id = $1 AND id = $2;

-- name: CountExtRecords :one
SELECT count(*) FROM ext_records WHERE node_id = $1;

-- name: CreateExtRecord :one
INSERT INTO ext_records (node_id, collection, data, created_by, updated_by)
VALUES (@node_id, @collection, @data, @by, @by)
RETURNING node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at;

-- name: ReplaceExtRecord :one
-- Replaces the data, when if_rev (if given) is still the record's rev.
UPDATE ext_records
SET data = @data, rev = rev + 1, updated_by = @by, updated_at = now()
WHERE node_id = @node_id AND id = @id
  AND (sqlc.narg(if_rev)::bigint IS NULL OR rev = sqlc.narg(if_rev)::bigint)
RETURNING node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at;

-- name: MergeExtRecord :one
-- Shallow merge: the patch's keys replace the record's, and a key the patch
-- sets to null is removed (JSON merge patch, one level deep).
UPDATE ext_records
SET data = (data || @patch::jsonb)
           - COALESCE((SELECT array_agg(e.key) FROM jsonb_each(@patch::jsonb) e
                        WHERE e.value = 'null'::jsonb), '{}'::text[]),
    rev = rev + 1, updated_by = @by, updated_at = now()
WHERE node_id = @node_id AND id = @id
  AND (sqlc.narg(if_rev)::bigint IS NULL OR rev = sqlc.narg(if_rev)::bigint)
RETURNING node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at;

-- name: DeleteExtRecord :one
DELETE FROM ext_records WHERE node_id = $1 AND id = $2
RETURNING collection;

-- name: CopyExtRecords :exec
-- A copy of a node holds its records under the same ids.
INSERT INTO ext_records (node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at)
SELECT @dst::uuid, id, collection, data, 1, created_by, updated_by, created_at, updated_at
FROM ext_records WHERE node_id = @src::uuid;

-- name: ListAppVisibleUsers :many
-- The people among ids an app on node may name: members of the node's space,
-- and whoever has written one of its records.
SELECT u.id, u.email, u.name, u.display_name
FROM users u
WHERE u.id = ANY(@ids::uuid[])
  AND (EXISTS (SELECT 1 FROM space_members m WHERE m.space_id = @space_id AND m.user_id = u.id)
       OR EXISTS (SELECT 1 FROM ext_records r
                   WHERE r.node_id = @node_id AND (r.created_by = u.id OR r.updated_by = u.id)));
