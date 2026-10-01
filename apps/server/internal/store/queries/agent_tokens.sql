-- name: CreateAgentToken :one
INSERT INTO agent_tokens (user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- Resolves a bearer token to its row and owner in one indexed read. Expiry is
-- filtered here, so an expired token is simply not found.
-- name: GetAgentTokenByHash :one
SELECT sqlc.embed(t), sqlc.embed(u)
FROM agent_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.expires_at > now();

-- name: TouchAgentToken :exec
UPDATE agent_tokens SET last_used_at = now() WHERE id = $1;

-- name: ListUserAgentTokens :many
SELECT * FROM agent_tokens
WHERE user_id = $1 AND expires_at > now()
ORDER BY created_at DESC;

-- user_id is part of the predicate so one person can never revoke another's
-- token by guessing an id.
-- name: DeleteUserAgentToken :execrows
DELETE FROM agent_tokens WHERE id = $1 AND user_id = $2;

-- name: DeleteExpiredAgentTokens :execrows
DELETE FROM agent_tokens WHERE expires_at <= now();
