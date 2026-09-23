-- The node an event concerns, as a column rather than a key inside `detail`,
-- so "who read folder X" is an indexed filter and not a scan of JSON.
--
-- Deliberately no foreign key to `nodes`. A purged file must not erase or
-- null out the record that someone read it — the same reasoning as
-- `user_id ... ON DELETE SET NULL`, taken one step further: here even the id
-- has to survive, because it is the only thing left to filter on once the
-- node is gone.
ALTER TABLE security_events ADD COLUMN node_id UUID;

-- Grant and share events already carried the node in detail.node; lift it
-- into the column so they answer the same filter. The pattern guard keeps a
-- malformed value from failing the migration.
UPDATE security_events SET node_id = (detail->>'node')::uuid
 WHERE detail->>'node' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

CREATE INDEX security_events_by_node ON security_events (node_id, created_at DESC)
    WHERE node_id IS NOT NULL;
