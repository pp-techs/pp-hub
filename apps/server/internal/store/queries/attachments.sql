-- Attachments are blob references owned by a document (migration 0013). The
-- ref_count trigger on node_attachments counts them; nothing here moves it.

-- name: AddAttachment :one
-- The same bytes attached twice to one document are one attachment: the
-- markdown names it by hash, so a second row could not be told apart. The
-- no-op update returns the row that is already there.
INSERT INTO node_attachments (node_id, blob_hash, name, mime, size, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (node_id, blob_hash) DO UPDATE SET name = node_attachments.name
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM node_attachments WHERE node_id = $1 AND blob_hash = $2 LIMIT 1;

-- name: CopyAttachments :exec
-- A copied document takes its attachments with it, authored by whoever made
-- the copy, as its version is.
INSERT INTO node_attachments (node_id, blob_hash, name, mime, size, created_by)
SELECT @dest_id, a.blob_hash, a.name, a.mime, a.size, @created_by
FROM node_attachments a
WHERE a.node_id = @src_id
ON CONFLICT (node_id, blob_hash) DO NOTHING;

-- name: ListAttachments :many
SELECT * FROM node_attachments WHERE node_id = $1 ORDER BY created_at, blob_hash;

-- name: ListAttachmentHolders :many
-- The live documents other than exclude_id that hold a blob as an
-- attachment: where a reference pasted from another document can be adopted
-- from, if its writer may download one of them. Bounded: one readable holder
-- is enough, and a blob pasted into thousands of documents is not a reason
-- for a write-back to resolve access thousands of times.
SELECT sqlc.embed(n), sqlc.embed(a)
FROM node_attachments a
JOIN nodes n ON n.id = a.node_id
WHERE a.blob_hash = @blob_hash
  AND a.node_id <> @exclude_id
  AND n.deleted_at IS NULL
ORDER BY a.created_at
LIMIT 32;
