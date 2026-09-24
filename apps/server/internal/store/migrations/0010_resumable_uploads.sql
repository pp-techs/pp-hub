-- M30: resumable (tus) uploads, and a used_bytes that counts what is on disk.
--
-- `uploads` has been in the baseline since 0001 and nothing ever wrote it, so
-- it is reshaped here without touching data:
--   * mime goes — the version's type is derived from the name at commit, as
--     it always was for the multipart path; a second copy could only drift.
--   * total_size is required. The creation request must declare its length:
--     the upload cap and the quota are checked against it before a byte is
--     accepted, which a deferred length would make impossible.
--   * node_id names the file an upload replaces (a new version) rather than
--     a new file beside it.
--   * completed_at marks a committed upload. The row outlives the commit
--     until it expires so a client that lost the final response can ask
--     (HEAD) and learn the file landed, instead of uploading it again.
ALTER TABLE uploads DROP COLUMN mime;
ALTER TABLE uploads ALTER COLUMN total_size SET NOT NULL;
ALTER TABLE uploads ADD COLUMN node_id UUID REFERENCES nodes(id) ON DELETE CASCADE;
ALTER TABLE uploads ADD COLUMN completed_at TIMESTAMPTZ;
ALTER TABLE uploads ADD CONSTRAINT uploads_offset_bounds
    CHECK (uploaded >= 0 AND uploaded <= total_size);

CREATE INDEX uploads_expiry ON uploads (expires_at);
CREATE INDEX uploads_pending_by_space ON uploads (space_id) WHERE completed_at IS NULL;

-- used_bytes now counts every stored version, trash included (SumSpaceUsage).
-- Recompute once so the meter does not show the old, smaller figure until the
-- next write to each space.
UPDATE spaces s
   SET used_bytes = COALESCE((
       SELECT SUM(v.size)
         FROM node_versions v
         JOIN nodes n ON n.id = v.node_id
        WHERE n.space_id = s.id), 0);
