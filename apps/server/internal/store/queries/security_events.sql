-- name: RecordSecurityEvent :exec
INSERT INTO security_events (kind, user_id, subject, ip, user_agent, request_id, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- The console's tail. The actor's email is joined in so a row still reads as a
-- person rather than a UUID, and falls back to the recorded subject when the
-- account is gone.
-- name: ListSecurityEvents :many
SELECT
  e.id, e.kind, e.user_id, e.subject, e.ip, e.user_agent, e.request_id,
  e.detail, e.created_at,
  COALESCE(u.email, '') AS user_email,
  COALESCE(u.display_name, u.name, '') AS user_name
FROM security_events e
LEFT JOIN users u ON u.id = e.user_id
WHERE (sqlc.narg(kind)::text IS NULL OR e.kind = sqlc.narg(kind)::text)
ORDER BY e.created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountSecurityEvents :one
SELECT COUNT(*) FROM security_events
WHERE (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text);

-- Retention. Security rows are small but they are written on every sign-in, so
-- something has to bound them; the sweep that purges expired sessions does
-- this too.
-- name: DeleteSecurityEventsBefore :execrows
DELETE FROM security_events WHERE created_at < $1;
