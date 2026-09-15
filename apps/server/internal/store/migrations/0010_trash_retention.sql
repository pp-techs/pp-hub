-- M11: Trash retention.
--
-- Two things the trash screen promises but could not deliver: who deleted an
-- item, and when it will be purged automatically.

-- Nullable, and NULL for everything already in the trash: there is no honest
-- way to attribute a delete that happened before this column existed.
ALTER TABLE nodes ADD COLUMN deleted_by UUID REFERENCES users(id) ON DELETE SET NULL;

-- Retention is per-space so a team can keep a longer or shorter bin than the
-- default. 0 disables automatic purging entirely, which is the safe value to
-- carry existing spaces forward on: nobody consented to their trash being
-- emptied by a migration.
ALTER TABLE spaces ADD COLUMN trash_retention_days INT NOT NULL DEFAULT 0;

-- The purge sweep asks "what is past its retention", which is a scan over
-- tombstones only — a partial index keeps it off the live rows.
CREATE INDEX nodes_trash_sweep ON nodes (space_id, deleted_at)
    WHERE deleted_at IS NOT NULL;
