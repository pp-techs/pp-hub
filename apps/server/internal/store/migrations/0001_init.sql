-- pp-hub consolidated baseline schema (M1-M15).
-- All tables, enums, triggers, and indices up to owner-anchored permission model.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ------------------------------------------------------------------ Enums ---

CREATE TYPE job_status AS ENUM (
    'pending',
    'running',
    'done',
    'failed'
);

CREATE TYPE node_kind AS ENUM (
    'dir',
    'file'
);

CREATE TYPE share_kind AS ENUM (
    'link',
    'direct',
    'group'
);

CREATE TYPE share_level AS ENUM (
    'view',
    'edit'
);

-- ---------------------------------------------------------------- Users ---

CREATE TABLE users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL DEFAULT 'default',
    oidc_sub     TEXT NOT NULL,
    oidc_iss     TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    avatar_url   TEXT NOT NULL DEFAULT '',
    bio          TEXT NOT NULL DEFAULT '',
    settings     JSONB NOT NULL DEFAULT '{}',
    is_admin     BOOLEAN NOT NULL DEFAULT false,
    last_seen    TIMESTAMPTZ,
    email_hash   TEXT NOT NULL DEFAULT '',
    suspended_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (oidc_iss, oidc_sub)
);

CREATE UNIQUE INDEX users_tenant_email_hash ON users (tenant_id, email_hash) WHERE email_hash <> '';

CREATE OR REPLACE FUNCTION users_email_hash_trigger() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.email_hash := LOWER(ENCODE(SHA256(NEW.email::bytea), 'hex'));
  RETURN NEW;
END;
$$;

CREATE TRIGGER users_email_hash
BEFORE INSERT OR UPDATE OF email ON users
FOR EACH ROW EXECUTE FUNCTION users_email_hash_trigger();

-- ---------------------------------------------------------------- Spaces ---

CREATE TABLE spaces (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            TEXT NOT NULL DEFAULT 'default',
    name                 TEXT NOT NULL,
    owner_id             UUID NOT NULL REFERENCES users(id),
    quota_bytes          BIGINT NOT NULL DEFAULT 0,
    used_bytes           BIGINT NOT NULL DEFAULT 0,
    slug                 TEXT NOT NULL DEFAULT '',
    description          TEXT NOT NULL DEFAULT '',
    archived_at          TIMESTAMPTZ,
    trash_retention_days INT NOT NULL DEFAULT 0,
    archive_root_id      UUID,
    default_as_admin     BOOLEAN NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Ownership is intrinsic, so whoever creates a node can always share it —
    -- including as a public link — whatever the grant on the folder above
    -- said about re-sharing. Right for a personal top level, wrong for a
    -- company that publishes nothing, so the switch lives where the policy
    -- does: on the space.
    allow_public_links   BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX spaces_slug_uniq ON spaces (tenant_id, slug) WHERE slug <> '';

CREATE TABLE space_members (
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id)
);

CREATE INDEX space_members_by_user ON space_members (user_id);

CREATE TABLE space_invitations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    token_hash  TEXT NOT NULL,
    invited_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    as_admin    BOOLEAN NOT NULL DEFAULT false,
    expires_at  TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX space_invitations_token ON space_invitations (token_hash);
CREATE UNIQUE INDEX space_invitations_pending ON space_invitations (space_id, lower(email)) WHERE accepted_at IS NULL;
CREATE INDEX space_invitations_by_email ON space_invitations (lower(email)) WHERE accepted_at IS NULL;

-- ---------------------------------------------------------------- Groups ---

CREATE TABLE groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_everyone BOOLEAN NOT NULL DEFAULT false,
    is_admin    BOOLEAN NOT NULL DEFAULT false,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX groups_name_uniq ON groups (space_id, lower(name));
CREATE UNIQUE INDEX groups_everyone_uniq ON groups (space_id) WHERE is_everyone;
CREATE UNIQUE INDEX groups_admin_uniq ON groups (space_id) WHERE is_admin;

CREATE TABLE group_members (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_members_by_user ON group_members (user_id);

-- ----------------------------------------------------------- Nodes & CAS ---

CREATE TABLE nodes (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          TEXT NOT NULL DEFAULT 'default',
    space_id           UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_id          UUID REFERENCES nodes(id) ON DELETE CASCADE,
    kind               node_kind NOT NULL,
    name               TEXT NOT NULL,
    current_version_id UUID,
    deleted_at         TIMESTAMPTZ,
    deleted_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    created_by         UUID NOT NULL REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Team folders: a folder the *team* owns rather than a person.
    --
    -- Everything else here hangs off `created_by`, the one unconditional
    -- access path: create something and you reach it forever, and so does
    -- whoever owns a folder above it. Right for a member's own top level,
    -- wrong for shared company content — it is why revoking a grant cannot
    -- take back what the grantee uploaded, why they can always mint a link
    -- for it, and why they can move it out from under the folder's owner.
    -- Drive, ownCloud and Nextcloud all grew a second container (shared
    -- drive, project space, team folder) where content has no personal owner.
    --
    -- NULL for personal content, the container's id for everything inside
    -- one, and its own id on the container itself. Denormalised on purpose:
    -- the alternative is an ancestor walk inside every resolution query, and
    -- this turns three recursive checks into a column predicate. `fs`
    -- maintains it — inherited from the parent on create and copy, rewritten
    -- across the subtree on move.
    team_root_id       UUID REFERENCES nodes(id) ON DELETE SET NULL,

    -- A node parented to itself makes every ancestor-walking CTE loop
    -- forever: NodeAccessCaps uses UNION ALL, so resolution for anything
    -- beneath the cycle never returns and the request holds its connection
    -- until the client gives up. `RemoveMember` once created exactly that
    -- shape — the space creator owns the Archive, so it came back as one of
    -- "their roots" and was filed under itself.
    CONSTRAINT nodes_no_self_parent_ck CHECK (parent_id IS DISTINCT FROM id),

    -- A team folder is always top level. Nesting one would make "which
    -- container owns this" ambiguous, and the whole point is that the answer
    -- is never in doubt.
    CONSTRAINT nodes_team_root_top_level_ck
        CHECK (team_root_id IS DISTINCT FROM id OR parent_id IS NULL)
);

CREATE UNIQUE INDEX nodes_name_uniq
    ON nodes (space_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
    WHERE deleted_at IS NULL;

CREATE INDEX nodes_listing ON nodes (space_id, parent_id) WHERE deleted_at IS NULL;
CREATE INDEX nodes_trash_sweep ON nodes (space_id, deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX nodes_name_trgm ON nodes USING gin (name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX nodes_team_root ON nodes (team_root_id) WHERE team_root_id IS NOT NULL;

-- Wire circular FK for spaces.archive_root_id
ALTER TABLE spaces ADD CONSTRAINT spaces_archive_root_id_fkey
    FOREIGN KEY (archive_root_id) REFERENCES nodes(id) ON DELETE SET NULL;

CREATE TABLE node_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    blob_hash  TEXT NOT NULL,
    size       BIGINT NOT NULL,
    mime       TEXT NOT NULL DEFAULT 'application/octet-stream',
    version_no INT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, version_no)
);

ALTER TABLE nodes ADD CONSTRAINT nodes_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES node_versions(id);

CREATE TABLE blobs (
    hash       TEXT PRIMARY KEY,
    size       BIGINT NOT NULL,
    ref_count  INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE uploads (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL DEFAULT 'default',
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES nodes(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    mime       TEXT NOT NULL DEFAULT 'application/octet-stream',
    total_size BIGINT,
    uploaded   BIGINT NOT NULL DEFAULT 0,
    temp_path  TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE node_stars (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, node_id)
);

CREATE INDEX node_stars_by_user ON node_stars (user_id, created_at DESC);

-- --------------------------------------------------------------- Shares ---

CREATE TABLE shares (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT NOT NULL DEFAULT 'default',
    node_id          UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind             share_kind NOT NULL,
    token_hash       TEXT,
    grantee_user_id  UUID REFERENCES users(id) ON DELETE CASCADE,
    grantee_group_id UUID REFERENCES groups(id) ON DELETE CASCADE,
    level            share_level NOT NULL,
    can_download     BOOLEAN NOT NULL,
    can_share        BOOLEAN NOT NULL,
    expires_at       TIMESTAMPTZ,
    created_by       UUID NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT shares_grantee_ck CHECK (
        (kind = 'link'   AND token_hash IS NOT NULL AND grantee_user_id IS NULL AND grantee_group_id IS NULL) OR
        (kind = 'direct' AND grantee_user_id  IS NOT NULL AND grantee_group_id IS NULL) OR
        (kind = 'group'  AND grantee_group_id IS NOT NULL AND grantee_user_id  IS NULL)
    ),
    CONSTRAINT shares_edit_download_ck CHECK (level <> 'edit' OR can_download),
    CONSTRAINT shares_link_ck CHECK (kind <> 'link' OR (level = 'view' AND NOT can_share))
);

CREATE UNIQUE INDEX shares_token_uniq ON shares (token_hash) WHERE token_hash IS NOT NULL;
CREATE UNIQUE INDEX shares_group_uniq ON shares (node_id, grantee_group_id) WHERE kind = 'group';
CREATE INDEX shares_node ON shares (node_id);
CREATE INDEX shares_grantee ON shares (grantee_user_id) WHERE grantee_user_id IS NOT NULL;
CREATE INDEX shares_by_group ON shares (grantee_group_id, node_id) WHERE kind = 'group';

-- ---------------------------------------------------- Locks, Jobs, Activity ---

CREATE TABLE file_locks (
    node_id    UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    lock_token TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE jobs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL DEFAULT 'default',
    kind       TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    status     job_status NOT NULL DEFAULT 'pending',
    run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts   INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX jobs_claim ON jobs (kind, status, run_at) WHERE status = 'pending';

CREATE TABLE previews (
    node_version_id UUID NOT NULL REFERENCES node_versions(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,
    blob_hash       TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_version_id, kind)
);

CREATE TABLE activity_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL DEFAULT 'default',
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    actor_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    UUID REFERENCES nodes(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL,
    node_name  TEXT NOT NULL DEFAULT '',
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX activity_by_space ON activity_events (space_id, created_at DESC);
CREATE INDEX activity_by_actor ON activity_events (actor_id, created_at DESC);
CREATE INDEX activity_by_node  ON activity_events (node_id, created_at DESC) WHERE node_id IS NOT NULL;
