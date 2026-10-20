-- ref_count is maintained by triggers on node_versions, previews,
-- users.avatar_blob (migration 0011), node_attachments (0013),
-- spaces.avatar_blob (0017) and publication_files (0030). Nothing here moves
-- it by hand.

-- name: ClaimBlob :one
-- Records a hash before its bytes are installed and pins it against GC for
-- long enough to install them and commit the reference. Runs on its own,
-- outside any transaction, so a concurrent sweep sees the pin at once: the
-- sweep's DELETE re-checks pinned_until after waiting for this row lock.
-- Reports whether the stored copy is known damaged and must be overwritten.
INSERT INTO blobs (hash, size, pinned_until)
VALUES (@hash, @size, now() + INTERVAL '6 hours')
ON CONFLICT (hash) DO UPDATE SET pinned_until = GREATEST(blobs.pinned_until, EXCLUDED.pinned_until)
RETURNING (damaged_at IS NOT NULL)::boolean AS damaged;

-- name: MarkBlobHealed :exec
-- A write replaced a damaged copy with bytes that hash to the name.
UPDATE blobs SET damaged_at = NULL, verified_at = now() WHERE hash = $1;

-- name: GetBlob :one
SELECT * FROM blobs WHERE hash = $1 LIMIT 1;

-- name: ListCollectableBlobs :many
-- Unreferenced for over an hour and not pinned by a write in progress. The
-- NOT EXISTS clauses repeat what ref_count already says; they keep a count
-- that is ever wrong from deleting bytes something still points at.
SELECT b.hash, b.size FROM blobs b
WHERE b.ref_count = 0
  AND b.released_at < now() - INTERVAL '1 hour'
  AND b.pinned_until < now()
  AND NOT EXISTS (SELECT 1 FROM node_versions v WHERE v.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM previews p WHERE p.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = b.hash)
  AND NOT EXISTS (SELECT 1 FROM spaces s WHERE s.avatar_blob = b.hash)
  AND NOT EXISTS (SELECT 1 FROM node_attachments a WHERE a.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM publication_files pf WHERE pf.blob_hash = b.hash)
ORDER BY b.released_at
LIMIT $1;

-- name: DeleteCollectableBlob :one
-- ListCollectableBlobs' condition, re-checked under the row lock. The caller
-- removes the bytes in the same transaction, before committing, so a claim
-- waiting on this row sees either a live row or no row and no file.
DELETE FROM blobs b
WHERE b.hash = $1
  AND b.ref_count = 0
  AND b.released_at < now() - INTERVAL '1 hour'
  AND b.pinned_until < now()
  AND NOT EXISTS (SELECT 1 FROM node_versions v WHERE v.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM previews p WHERE p.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.avatar_blob = b.hash)
  AND NOT EXISTS (SELECT 1 FROM spaces s WHERE s.avatar_blob = b.hash)
  AND NOT EXISTS (SELECT 1 FROM node_attachments a WHERE a.blob_hash = b.hash)
  AND NOT EXISTS (SELECT 1 FROM publication_files pf WHERE pf.blob_hash = b.hash)
RETURNING b.size;

-- name: RecountBlobRefs :many
-- Sets every ref_count to the number of rows that reference the hash and
-- returns the rows it had to correct. The triggers keep the counts exact, so
-- anything returned is a bug worth a log line.
WITH refs AS (
    SELECT blob_hash AS hash, count(*) AS n FROM node_versions GROUP BY blob_hash
    UNION ALL
    SELECT blob_hash, count(*) FROM previews GROUP BY blob_hash
    UNION ALL
    SELECT avatar_blob, count(*) FROM users WHERE avatar_blob <> '' GROUP BY avatar_blob
    UNION ALL
    SELECT avatar_blob, count(*) FROM spaces WHERE avatar_blob <> '' GROUP BY avatar_blob
    UNION ALL
    SELECT blob_hash, count(*) FROM node_attachments GROUP BY blob_hash
    UNION ALL
    SELECT blob_hash, count(*) FROM publication_files GROUP BY blob_hash
), actual AS (
    SELECT b2.hash, COALESCE(sum(r.n), 0)::int AS n
    FROM blobs b2 LEFT JOIN refs r ON r.hash = b2.hash
    GROUP BY b2.hash
)
UPDATE blobs b
   SET ref_count   = a.n,
       released_at = CASE WHEN a.n = 0 THEN now() ELSE b.released_at END
  FROM actual a
 WHERE a.hash = b.hash AND a.n <> b.ref_count
RETURNING b.hash, b.ref_count;

-- name: KnownBlobHashes :many
-- Which of these hashes have a row. The storage audit asks in batches while
-- walking the store.
SELECT hash FROM blobs WHERE hash = ANY(@hashes::text[]);

-- name: AdoptBlob :execrows
-- Gives bytes found in the store without a row one, counting whatever
-- already references them. Unreferenced bytes are then collected through
-- the ordinary sweep, under its guards, rather than deleted by the audit.
INSERT INTO blobs (hash, size, ref_count)
SELECT @hash::text, @size::bigint,
       ((SELECT count(*) FROM node_versions v WHERE v.blob_hash = @hash::text)
      + (SELECT count(*) FROM previews p WHERE p.blob_hash = @hash::text)
      + (SELECT count(*) FROM users u WHERE u.avatar_blob = @hash::text)
      + (SELECT count(*) FROM spaces s WHERE s.avatar_blob = @hash::text)
      + (SELECT count(*) FROM node_attachments a WHERE a.blob_hash = @hash::text)
      + (SELECT count(*) FROM publication_files pf WHERE pf.blob_hash = @hash::text))::int
ON CONFLICT (hash) DO NOTHING;

-- name: MarkBlobVerified :exec
UPDATE blobs SET verified_at = now(), damaged_at = NULL WHERE hash = $1;

-- name: ListBlobsToVerify :many
-- The scrub's queue: never verified first, then longest ago. Garbage awaiting
-- GC is not worth the read, and a pinned row may be a write whose bytes are
-- still on their way in.
SELECT hash, size FROM blobs
WHERE ref_count > 0 AND pinned_until < now()
ORDER BY verified_at NULLS FIRST
LIMIT $1;

-- name: MarkBlobDamaged :exec
UPDATE blobs SET verified_at = now(), damaged_at = COALESCE(damaged_at, now()) WHERE hash = $1;

-- name: BlobTotals :one
-- Referenced vs. awaiting-GC blob counts and bytes, and damaged ones, for
-- /metrics.
SELECT count(*) FILTER (WHERE ref_count > 0)::bigint AS referenced_blobs,
       COALESCE(sum(size) FILTER (WHERE ref_count > 0), 0)::bigint AS referenced_bytes,
       count(*) FILTER (WHERE ref_count = 0)::bigint AS unreferenced_blobs,
       COALESCE(sum(size) FILTER (WHERE ref_count = 0), 0)::bigint AS unreferenced_bytes,
       count(*) FILTER (WHERE damaged_at IS NOT NULL)::bigint AS damaged_blobs
FROM blobs;
