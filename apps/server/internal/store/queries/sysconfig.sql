-- name: GetSystemConfig :one
SELECT * FROM system_config
WHERE key = $1 AND tenant_id = $2;

-- name: ListSystemConfig :many
SELECT * FROM system_config
WHERE tenant_id = $1
ORDER BY key ASC;

-- name: UpsertSystemConfig :one
INSERT INTO system_config (key, tenant_id, value, updated_at, updated_by)
VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    tenant_id = EXCLUDED.tenant_id,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: DeleteSystemConfig :exec
DELETE FROM system_config
WHERE key = $1 AND tenant_id = $2;
