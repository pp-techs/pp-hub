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
