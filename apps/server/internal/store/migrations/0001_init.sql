-- M1 skeleton: users (JIT provisioned from OIDC logins).
-- tenant_id is present from day one per docs/architecture/data-model.md.

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    oidc_sub    TEXT NOT NULL,
    oidc_iss    TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (oidc_iss, oidc_sub)
);
