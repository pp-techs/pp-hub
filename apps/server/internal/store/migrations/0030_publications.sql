-- Publishing (M50, docs/architecture/publishing.md): an .html file or a
-- folder becomes a public mini site served from PP_ARTIFACT_URL.
--
-- A publication is a snapshot. Its files are rows naming blobs that are
-- already in the CAS, so publishing copies no bytes; editing the source
-- changes nothing public until a republish replaces the rows and bumps rev.
CREATE TABLE publications (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL DEFAULT 'default',
    space_id      UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    -- The source. One publication per node; a purge of the source ends it,
    -- trashing does not.
    node_id       UUID NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE CASCADE,
    -- The source's name at publish time, for the viewer's bar.
    title         TEXT NOT NULL,
    entry_path    TEXT NOT NULL,
    -- sha256 (hex) of the token: the lookup.
    token_hash    TEXT NOT NULL UNIQUE,
    -- The token itself, AES-GCM under DeriveKey("publication-token-v1"), so
    -- the dialog can show the URL again.
    token_sealed  TEXT NOT NULL,
    rev           INT NOT NULL DEFAULT 1,
    published_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULL = no password. PBKDF2-SHA256, encoded with its parameters.
    password_hash TEXT,
    -- NULL = never.
    expires_at    TIMESTAMPTZ,
    created_by    UUID NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX publications_space ON publications (space_id);
CREATE INDEX publications_expiry ON publications (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE publication_files (
    publication_id UUID NOT NULL REFERENCES publications(id) ON DELETE CASCADE,
    path           TEXT NOT NULL,
    blob_hash      TEXT NOT NULL REFERENCES blobs(hash),
    size           BIGINT NOT NULL,
    mime           TEXT NOT NULL,
    PRIMARY KEY (publication_id, path)
);

CREATE INDEX publication_files_blob_hash ON publication_files (blob_hash);

-- Blob references, as node_attachments_blob_ref (0013): the count moves in
-- the same statement as the reference, cascades included, so GC never takes
-- a blob a publication still serves — even after its source is purged.
CREATE FUNCTION publication_files_blob_ref() RETURNS trigger
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

CREATE TRIGGER publication_files_blob_ref_ins AFTER INSERT ON publication_files
FOR EACH ROW EXECUTE FUNCTION publication_files_blob_ref();
CREATE TRIGGER publication_files_blob_ref_del AFTER DELETE ON publication_files
FOR EACH ROW EXECUTE FUNCTION publication_files_blob_ref();
CREATE TRIGGER publication_files_blob_ref_upd AFTER UPDATE OF blob_hash ON publication_files
FOR EACH ROW WHEN (OLD.blob_hash IS DISTINCT FROM NEW.blob_hash)
EXECUTE FUNCTION publication_files_blob_ref();

-- spaces.used_bytes, as 0028 keeps it for versions and attachments: a
-- snapshot's bytes count against its space. File rows move the figure by
-- their size while their publication exists (a republish replaces them). A
-- publication being deleted — unpublish, expiry, or the cascade from a purge
-- of its source or of the space — subtracts its files once, BEFORE DELETE,
-- and the cascaded file deletes that follow find it gone and skip.
CREATE FUNCTION publication_usage_add() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE spaces SET used_bytes = used_bytes + NEW.size
     WHERE id = (SELECT space_id FROM publications WHERE id = NEW.publication_id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION publication_usage_remove() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sid UUID;
BEGIN
    SELECT space_id INTO sid FROM publications WHERE id = OLD.publication_id;
    IF FOUND THEN
        UPDATE spaces SET used_bytes = GREATEST(used_bytes - OLD.size, 0) WHERE id = sid;
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION publication_usage_purge() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE spaces
       SET used_bytes = GREATEST(used_bytes
           - (SELECT COALESCE(SUM(size), 0) FROM publication_files WHERE publication_id = OLD.id), 0)
     WHERE id = OLD.space_id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER publication_files_usage_ins AFTER INSERT ON publication_files FOR EACH ROW EXECUTE FUNCTION publication_usage_add();
CREATE TRIGGER publication_files_usage_del AFTER DELETE ON publication_files FOR EACH ROW EXECUTE FUNCTION publication_usage_remove();
CREATE TRIGGER publications_usage_purge BEFORE DELETE ON publications FOR EACH ROW EXECUTE FUNCTION publication_usage_purge();
