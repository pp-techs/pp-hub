-- M44: the extension host (docs/architecture/extensions.md).

-- Which extension's node this is ("kanban.board"). Exactly the 'ext' nodes
-- carry one, so a listing can tell the two apart without a join.
ALTER TABLE nodes ADD COLUMN ext_kind TEXT;
ALTER TABLE nodes ADD CONSTRAINT nodes_ext_kind_ck
    CHECK ((kind = 'ext') = (ext_kind IS NOT NULL));

-- The on/off switches. A row with a NULL space is the instance switch; a row
-- with a space can only turn an instance-enabled extension off for that space.
-- No instance row means off: extensions ship disabled.
CREATE TABLE ext_state (
    ext_id     TEXT NOT NULL,
    space_id   UUID REFERENCES spaces(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ext_state_instance ON ext_state (ext_id) WHERE space_id IS NULL;
CREATE UNIQUE INDEX ext_state_space ON ext_state (ext_id, space_id) WHERE space_id IS NOT NULL;

-- Each extension owns a Postgres schema ext_<id> and its own migration track,
-- applied by cmd/server after the core's.
CREATE TABLE ext_migrations (
    ext_id     TEXT NOT NULL,
    version    TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (ext_id, version)
);

-- An extension's own notification kinds are "ext:<id>:<verb>".
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check
    CHECK (kind IN ('comment', 'reply', 'mention') OR kind LIKE 'ext:%');
