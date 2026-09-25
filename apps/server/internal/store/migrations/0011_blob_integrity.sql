-- Blob integrity: reference counts the database keeps, claims that pin a blob
-- while its bytes are being written, and a record of what the scrub found.
--
-- Until now the application incremented and decremented blobs.ref_count by
-- hand, in statements separate from the rows that are the references. A
-- failure between the two left a count too low, and GC deletes on the count.
-- Previews were never counted at all, and a cascade delete cannot call back
-- into Go. Triggers on the three tables that reference a blob make the count
-- change in the same statement as the reference, cascades included.

ALTER TABLE blobs
    -- When ref_count last reached 0. The GC grace period runs from here, not
    -- from created_at: an old blob that has just been released is exactly as
    -- fresh a candidate as a new one.
    ADD COLUMN released_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A writer claims the hash before installing the bytes (ClaimBlob). GC
    -- leaves a pinned row alone, so it can never delete a file that a write
    -- has just deduplicated against and not yet referenced.
    ADD COLUMN pinned_until TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Scrub bookkeeping. damaged_at is set when the bytes are missing or no
    -- longer hash to their name; the next write of the same content replaces
    -- them and clears it.
    ADD COLUMN verified_at  TIMESTAMPTZ,
    ADD COLUMN damaged_at   TIMESTAMPTZ;

CREATE INDEX node_versions_blob_hash ON node_versions (blob_hash);
CREATE INDEX previews_blob_hash ON previews (blob_hash);
CREATE INDEX users_avatar_blob ON users (avatar_blob) WHERE avatar_blob <> '';
CREATE INDEX blobs_collectable ON blobs (released_at) WHERE ref_count = 0;
CREATE INDEX blobs_verify_order ON blobs (verified_at NULLS FIRST);

-- blob_acquire / blob_release move one reference. A reference to a hash with
-- no row is not counted here; the storage audit adopts such rows with the
-- count computed from the referencing tables.
CREATE FUNCTION blob_acquire(h TEXT) RETURNS void
LANGUAGE sql AS $$
    UPDATE blobs SET ref_count = ref_count + 1 WHERE hash = h;
$$;

CREATE FUNCTION blob_release(h TEXT) RETURNS void
LANGUAGE sql AS $$
    UPDATE blobs
       SET ref_count   = GREATEST(ref_count - 1, 0),
           released_at = CASE WHEN ref_count <= 1 THEN now() ELSE released_at END
     WHERE hash = h;
$$;

-- A version knows its size, so it can create the row it references. Every
-- writer claims first, so in practice the row exists; creating it here means
-- no path can reference bytes GC does not know are referenced.
CREATE FUNCTION node_versions_blob_ref() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        INSERT INTO blobs (hash, size, ref_count) VALUES (NEW.blob_hash, NEW.size, 1)
        ON CONFLICT (hash) DO UPDATE SET ref_count = blobs.ref_count + 1;
    END IF;
    IF TG_OP IN ('DELETE', 'UPDATE') THEN
        PERFORM blob_release(OLD.blob_hash);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER node_versions_blob_ref_ins AFTER INSERT ON node_versions
FOR EACH ROW EXECUTE FUNCTION node_versions_blob_ref();
CREATE TRIGGER node_versions_blob_ref_del AFTER DELETE ON node_versions
FOR EACH ROW EXECUTE FUNCTION node_versions_blob_ref();
CREATE TRIGGER node_versions_blob_ref_upd AFTER UPDATE OF blob_hash ON node_versions
FOR EACH ROW WHEN (OLD.blob_hash IS DISTINCT FROM NEW.blob_hash)
EXECUTE FUNCTION node_versions_blob_ref();

CREATE FUNCTION previews_blob_ref() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM blob_acquire(NEW.blob_hash);
    END IF;
    IF TG_OP IN ('DELETE', 'UPDATE') THEN
        PERFORM blob_release(OLD.blob_hash);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER previews_blob_ref_ins AFTER INSERT ON previews
FOR EACH ROW EXECUTE FUNCTION previews_blob_ref();
CREATE TRIGGER previews_blob_ref_del AFTER DELETE ON previews
FOR EACH ROW EXECUTE FUNCTION previews_blob_ref();
CREATE TRIGGER previews_blob_ref_upd AFTER UPDATE OF blob_hash ON previews
FOR EACH ROW WHEN (OLD.blob_hash IS DISTINCT FROM NEW.blob_hash)
EXECUTE FUNCTION previews_blob_ref();

-- '' means "no avatar" and is never a reference.
CREATE FUNCTION users_avatar_blob_ref() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP IN ('INSERT', 'UPDATE') AND NEW.avatar_blob <> '' THEN
        PERFORM blob_acquire(NEW.avatar_blob);
    END IF;
    IF TG_OP IN ('DELETE', 'UPDATE') AND OLD.avatar_blob <> '' THEN
        PERFORM blob_release(OLD.avatar_blob);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER users_avatar_blob_ref_ins AFTER INSERT ON users
FOR EACH ROW EXECUTE FUNCTION users_avatar_blob_ref();
CREATE TRIGGER users_avatar_blob_ref_del AFTER DELETE ON users
FOR EACH ROW EXECUTE FUNCTION users_avatar_blob_ref();
CREATE TRIGGER users_avatar_blob_ref_upd AFTER UPDATE OF avatar_blob ON users
FOR EACH ROW WHEN (OLD.avatar_blob IS DISTINCT FROM NEW.avatar_blob)
EXECUTE FUNCTION users_avatar_blob_ref();

-- Start from counts that are true. Whatever the hand-maintained counts had
-- drifted to, this is what the triggers keep from here on.
UPDATE blobs b
   SET ref_count = a.n
  FROM (
      SELECT b2.hash,
             (SELECT count(*) FROM node_versions v WHERE v.blob_hash = b2.hash)
           + (SELECT count(*) FROM previews p WHERE p.blob_hash = b2.hash)
           + (SELECT count(*) FROM users u WHERE u.avatar_blob = b2.hash) AS n
        FROM blobs b2
  ) a
 WHERE a.hash = b.hash AND a.n <> b.ref_count;
