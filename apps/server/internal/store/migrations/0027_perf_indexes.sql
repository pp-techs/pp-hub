-- Indexes for the lookups that scanned whole tables.
--
-- Postgres indexes neither side of a foreign key on its own, so every cascade
-- below checked the referencing table row by row, and the hot filters on
-- parent_id, created_by and the jobs queue had only partial or space-led
-- indexes to choose from. Plain CREATE INDEX: the runner wraps each file in a
-- transaction, and the build blocks writes only once, at server start.

-- Child lookups: FK ON DELETE CASCADE from a purged folder, downward recursive
-- CTEs and HasChildren/CountChildPages all filter parent_id alone; the existing
-- indexes lead with space_id and are partial.
CREATE INDEX nodes_parent ON nodes (parent_id);
-- PruneVersions deletes versions; the FK check from nodes.current_version_id
-- otherwise scans nodes once per deleted version, on every save.
CREATE INDEX nodes_current_version ON nodes (current_version_id) WHERE current_version_id IS NOT NULL;
-- ListEntryPoints' owned branch and the other created_by filters.
CREATE INDEX nodes_owned_live ON nodes (created_by, space_id) WHERE team_root_id IS NULL AND deleted_at IS NULL;
-- Cascading FKs without an index on the referencing column.
CREATE INDEX node_stars_node ON node_stars (node_id);
CREATE INDEX uploads_parent ON uploads (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX uploads_node ON uploads (node_id) WHERE node_id IS NOT NULL;
CREATE INDEX uploads_attach_to ON uploads (attach_to) WHERE attach_to IS NOT NULL;
CREATE INDEX spaces_archive_root ON spaces (archive_root_id) WHERE archive_root_id IS NOT NULL;
CREATE INDEX notifications_node ON notifications (node_id);
CREATE INDEX notifications_thread ON notifications (thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX notifications_comment ON notifications (comment_id) WHERE comment_id IS NOT NULL;
-- Recent: last action per node for one actor, index-only.
CREATE INDEX activity_by_actor_node ON activity_events (actor_id, node_id, created_at DESC) INCLUDE (kind) WHERE node_id IS NOT NULL;
-- jobs: the reaper, the done-row purge, and the backfill anti-joins.
CREATE INDEX jobs_running ON jobs (run_at) WHERE status = 'running';
CREATE INDEX jobs_done_created ON jobs (created_at) WHERE status = 'done';
CREATE INDEX jobs_inflight_version ON jobs ((payload->>'node_version_id')) WHERE status IN ('pending', 'running');
