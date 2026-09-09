-- name: UpsertUser :one
INSERT INTO users (oidc_sub, oidc_iss, email, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (oidc_iss, oidc_sub)
DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;
