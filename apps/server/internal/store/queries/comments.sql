-- name: CreateCommentThread :one
INSERT INTO comment_threads (node_id, created_by)
VALUES ($1, $2)
RETURNING *;

-- name: GetCommentThread :one
SELECT * FROM comment_threads WHERE id = $1;

-- name: LockCommentThread :one
-- Serializes the writes to one thread (a reply, a delete, a resolve): a
-- reply racing the deletion of the last comment must not land in a thread
-- that is being removed.
SELECT * FROM comment_threads WHERE id = $1 FOR UPDATE;

-- name: ListCommentThreads :many
SELECT * FROM comment_threads WHERE node_id = $1 ORDER BY created_at, id;

-- name: ResolveCommentThread :one
-- Idempotent: resolving a resolved thread keeps who resolved it, and when.
UPDATE comment_threads
SET resolved_at = COALESCE(resolved_at, now()),
    resolved_by = COALESCE(resolved_by, @user_id::uuid)
WHERE id = @id
RETURNING *;

-- name: ReopenCommentThread :one
UPDATE comment_threads SET resolved_at = NULL, resolved_by = NULL
WHERE id = $1
RETURNING *;

-- name: DeleteEmptyCommentThread :execrows
DELETE FROM comment_threads t
WHERE t.id = $1 AND NOT EXISTS (SELECT 1 FROM comments c WHERE c.thread_id = t.id);

-- name: CreateComment :one
INSERT INTO comments (thread_id, author_id, body)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCommentWithNode :one
SELECT sqlc.embed(c), t.node_id
FROM comments c
JOIN comment_threads t ON t.id = c.thread_id
WHERE c.id = $1;

-- name: UpdateCommentBody :one
UPDATE comments SET body = $2, edited_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteComment :execrows
DELETE FROM comments WHERE id = $1;

-- name: ListNodeComments :many
SELECT sqlc.embed(c), sqlc.embed(u)
FROM comments c
JOIN comment_threads t ON t.id = c.thread_id
JOIN users u ON u.id = c.author_id
WHERE t.node_id = $1
ORDER BY c.created_at, c.id;

-- name: ListThreadComments :many
SELECT sqlc.embed(c), sqlc.embed(u)
FROM comments c
JOIN users u ON u.id = c.author_id
WHERE c.thread_id = $1
ORDER BY c.created_at, c.id;

-- name: ListThreadParticipants :many
-- Everyone who wrote in the thread, and whoever opened it.
SELECT author_id AS user_id FROM comments WHERE thread_id = @thread_id
UNION
SELECT created_by FROM comment_threads WHERE id = @thread_id;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id = ANY(@ids::uuid[]);
