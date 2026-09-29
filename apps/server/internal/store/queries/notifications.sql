-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, actor_id, node_id, thread_id, comment_id, excerpt)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: CreateVersionMentionNotification :many
-- A document mention, once per person and version: the job that writes it
-- may run again after a crash, and the second run must not notify twice.
INSERT INTO notifications (user_id, kind, actor_id, node_id, version_id, excerpt)
VALUES ($1, 'mention', $2, $3, $4, $5)
ON CONFLICT (user_id, version_id) WHERE version_id IS NOT NULL DO NOTHING
RETURNING id;

-- name: ListNotifications :many
-- A user's notifications, newest first, keyset-paginated on (created_at, id).
-- A notification about a trashed document is hidden until it is restored.
SELECT n.id, n.user_id, n.kind, n.node_id, n.thread_id, n.comment_id, n.excerpt,
       n.created_at, n.read_at, nd.name AS node_name, nd.space_id, sqlc.embed(a)
FROM notifications n
JOIN nodes nd ON nd.id = n.node_id AND nd.deleted_at IS NULL
JOIN users a ON a.id = n.actor_id
WHERE n.user_id = @user_id
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (n.created_at, n.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY n.created_at DESC, n.id DESC
LIMIT @lim;

-- name: GetNotificationsByIDs :many
SELECT n.id, n.user_id, n.kind, n.node_id, n.thread_id, n.comment_id, n.excerpt,
       n.created_at, n.read_at, nd.name AS node_name, nd.space_id, sqlc.embed(a)
FROM notifications n
JOIN nodes nd ON nd.id = n.node_id AND nd.deleted_at IS NULL
JOIN users a ON a.id = n.actor_id
WHERE n.id = ANY(@ids::uuid[])
ORDER BY n.created_at, n.id;

-- name: CountUnreadNotificationsByNode :many
SELECT n.node_id, count(*)::bigint AS unread
FROM notifications n
JOIN nodes nd ON nd.id = n.node_id AND nd.deleted_at IS NULL
WHERE n.user_id = $1 AND n.read_at IS NULL
GROUP BY n.node_id;

-- name: MarkNotificationsRead :execrows
UPDATE notifications SET read_at = now()
WHERE user_id = @user_id AND read_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;

-- name: PurgeReadNotifications :execrows
DELETE FROM notifications WHERE read_at < $1;

-- name: GetPreviousVersion :one
-- The version a new one replaced: the newest with a lower number, if pruning
-- has left one.
SELECT * FROM node_versions
WHERE node_id = $1 AND version_no < $2
ORDER BY version_no DESC
LIMIT 1;
