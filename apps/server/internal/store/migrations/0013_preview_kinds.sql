-- Split the one word "kind" into the two things it was doing.
--
-- jobs.kind was both "how do I render this" and "what will the client ask
-- for", which forced kindForMime (Go) and previewKind (TS) to agree exactly or
-- every request 404s. Once PDF and video produce real PNGs they all produce a
-- *thumbnail*; how it was rendered is the worker's business alone.
--
--   jobs.kind     -> routing key: which worker can claim this
--   previews.kind -> artifact type: what the client asks for
--
-- See docs/architecture/preview-pipeline.md.

UPDATE jobs SET kind = 'render:' || kind WHERE kind IN ('pdf', 'video');
UPDATE jobs SET kind = 'render:image' WHERE kind = 'thumbnail';
UPDATE jobs SET kind = 'snippet:text' WHERE kind = 'text';

-- The pdf/video preview rows are placeholder strings ("PDF document — preview
-- placeholder"), never read by any caller. Dropping them lets the real
-- thumbnail take the (node_version_id, 'thumbnail') slot on backfill; blob GC
-- reclaims the orphaned bytes on its next sweep.
DELETE FROM previews WHERE kind IN ('pdf', 'video');

-- Claiming now filters by kind, so the partial index has to lead with it:
-- a worker that cannot rasterize must not claim (and then fail) a render:pdf.
DROP INDEX IF EXISTS jobs_claim;
CREATE INDEX jobs_claim ON jobs (kind, status, run_at) WHERE status = 'pending';
