-- M2: Drive core. See docs/architecture/data-model.md.

CREATE TABLE spaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    name        TEXT NOT NULL,
    owner_id    UUID NOT NULL REFERENCES users(id),
    quota_bytes BIGINT NOT NULL DEFAULT 0,           -- 0 = unlimited in MVP
    used_bytes  BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE member_role AS ENUM ('owner', 'editor', 'viewer');

CREATE TABLE space_members (
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        member_role NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id)
);

CREATE TYPE node_kind AS ENUM ('dir', 'file');

CREATE TABLE nodes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL DEFAULT 'default',
    space_id            UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_id           UUID REFERENCES nodes(id) ON DELETE CASCADE,  -- NULL = space root
    kind                node_kind NOT NULL,
    name                TEXT NOT NULL,
    current_version_id  UUID,                -- set after insert; NULL for dirs
    deleted_at          TIMESTAMPTZ,
    created_by          UUID NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Name uniqueness per parent among live nodes. NULL parent (roots) gets a
-- sentinel so the unique index covers space roots too.
CREATE UNIQUE INDEX nodes_name_uniq
    ON nodes (space_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
    WHERE deleted_at IS NULL;

CREATE INDEX nodes_listing ON nodes (space_id, parent_id) WHERE deleted_at IS NULL;

CREATE TABLE node_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id     UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    blob_hash   TEXT NOT NULL,
    size        BIGINT NOT NULL,
    mime        TEXT NOT NULL DEFAULT 'application/octet-stream',
    version_no  INT NOT NULL,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, version_no)
);

-- Deferred FK: nodes.current_version_id → node_versions.id
ALTER TABLE nodes
    ADD CONSTRAINT nodes_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES node_versions(id);

CREATE TABLE blobs (
    hash        TEXT PRIMARY KEY,            -- blake3 hex
    size        BIGINT NOT NULL,
    ref_count   INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Resumable upload sessions (tus-style, minimal).
CREATE TABLE uploads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL DEFAULT 'default',
    space_id        UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_id       UUID REFERENCES nodes(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    mime            TEXT NOT NULL DEFAULT 'application/octet-stream',
    total_size      BIGINT,                  -- NULL = unknown until final chunk
    uploaded        BIGINT NOT NULL DEFAULT 0,
    temp_path       TEXT NOT NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
