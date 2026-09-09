-- M3: Sharing. See docs/architecture/permissions.md.

CREATE TYPE share_kind AS ENUM ('link', 'direct');
CREATE TYPE share_perm AS ENUM ('view', 'download', 'edit');

CREATE TABLE shares (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL DEFAULT 'default',
    node_id         UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind            share_kind NOT NULL,
    token_hash      TEXT,                       -- sha256 hex; link only
    grantee_user_id UUID REFERENCES users(id) ON DELETE CASCADE,  -- direct only
    perm            share_perm NOT NULL,
    expires_at      TIMESTAMPTZ,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (kind = 'link'   AND token_hash IS NOT NULL AND grantee_user_id IS NULL) OR
        (kind = 'direct' AND grantee_user_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX shares_token_uniq ON shares (token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX shares_node ON shares (node_id);
CREATE INDEX shares_grantee ON shares (grantee_user_id) WHERE grantee_user_id IS NOT NULL;
