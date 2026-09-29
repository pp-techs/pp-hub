-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Resolves a cookie to its session and owner in one indexed read. The expiry
-- is filtered here rather than in Go so an expired row can never be touched
-- back to life by the refresh below.
-- name: GetSessionByHash :one
SELECT sqlc.embed(s), sqlc.embed(u)
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- Slides the idle deadline and, in the same statement, makes users.last_seen
-- mean what it says. It used to be written only at login, so the People table
-- was showing "last seen" for a value that was really "last signed in".
-- name: TouchSession :exec
WITH touched AS (
  UPDATE sessions
  SET last_used_at = now(), expires_at = $2
  WHERE sessions.id = $1
  RETURNING user_id
)
UPDATE users SET last_seen = now()
FROM touched WHERE users.id = touched.user_id;

-- Whether a session still authenticates its owner: unexpired, younger than
-- the absolute cap (created_after), not revoked, and the account not
-- suspended — what `authenticated` checks per request, asked again of a
-- connection that outlives its request (a collab socket).
-- name: SessionActive :one
SELECT EXISTS (
  SELECT 1 FROM sessions s
  JOIN users u ON u.id = s.user_id
  WHERE s.id = sqlc.arg(id) AND s.expires_at > now()
    AND s.created_at > sqlc.arg(created_after)::timestamptz
    AND u.suspended_at IS NULL
)::boolean AS active;

-- name: ListUserSessions :many
SELECT * FROM sessions
WHERE user_id = $1 AND expires_at > now()
ORDER BY last_used_at DESC;

-- user_id is part of the predicate so one person can never revoke another's
-- session by guessing an id.
-- name: DeleteUserSession :execrows
DELETE FROM sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteSessionByHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteOtherUserSessions :execrows
DELETE FROM sessions WHERE user_id = $1 AND id <> $2;

-- name: DeleteUserSessions :execrows
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();
