-- M8: Stars and activity.
--
-- Two tables, both append-mostly, both read far more often than written.

-- A star is a per-user bookmark, not a property of the node: two people can
-- disagree about whether a file matters. Composite PK gives idempotent
-- starring (ON CONFLICT DO NOTHING) and a natural index for "is this starred".
CREATE TABLE node_stars (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id     UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, node_id)
);

-- Listing one user's stars newest-first is the only other access pattern.
CREATE INDEX node_stars_by_user ON node_stars (user_id, created_at DESC);

-- Activity is the app's memory: what happened, to what, by whom.
--
-- node_id is nullable and ON DELETE SET NULL, so purging a file does not erase
-- the record that it once existed and was deleted — the feed keeps its own
-- copy of the name for exactly that reason. space_id cascades, because an
-- entire space going away should take its history with it.
CREATE TABLE activity_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    actor_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id     UUID REFERENCES nodes(id) ON DELETE SET NULL,
    -- Verb: uploaded, created, renamed, moved, trashed, restored, purged,
    -- shared, unshared, reverted, joined, invited. Text rather than an enum so
    -- a new verb is a code change, not a migration.
    kind        TEXT NOT NULL,
    -- Denormalised so the feed renders without joining nodes, and still reads
    -- correctly after the node is purged.
    node_name   TEXT NOT NULL DEFAULT '',
    -- Verb-specific extras (old name on a rename, target folder on a move).
    detail      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The feed is always "latest N", scoped to a space or to an actor.
CREATE INDEX activity_by_space ON activity_events (space_id, created_at DESC);
CREATE INDEX activity_by_actor ON activity_events (actor_id, created_at DESC);
-- "Recent" reads the caller's own touches of a given node, newest first.
CREATE INDEX activity_by_node ON activity_events (node_id, created_at DESC)
    WHERE node_id IS NOT NULL;
