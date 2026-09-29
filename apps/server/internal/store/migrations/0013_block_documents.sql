-- M39: block documents. A markdown file edited in the block editor owns the
-- images and files dropped into it, and an attachment upload has to remember
-- what it produced.
--
-- An attachment is a blob reference that belongs to its document rather than
-- to a folder: it is not a node, so it never shows in a listing, and it lives
-- exactly as long as the document does — older versions of the markdown may
-- still point at it, so no single version can own it. The markdown names it as
-- `attachment:<hash>`, which is why the hash is half of the key: copying a
-- document copies its rows and no markdown needs rewriting.
CREATE TABLE node_attachments (
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    blob_hash  TEXT NOT NULL REFERENCES blobs(hash),
    name       TEXT NOT NULL,
    mime       TEXT NOT NULL,
    size       BIGINT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, blob_hash)
);

CREATE INDEX node_attachments_blob_hash ON node_attachments (blob_hash);

-- An attachment row is a reference like a version, a preview or an avatar,
-- counted by trigger in the same statement — including the rows a purge
-- removes by cascade. The foreign key guarantees the blobs row exists (every
-- writer claims before it references), so acquiring never has to create it.
CREATE FUNCTION node_attachments_blob_ref() RETURNS trigger
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

CREATE TRIGGER node_attachments_blob_ref_ins AFTER INSERT ON node_attachments
FOR EACH ROW EXECUTE FUNCTION node_attachments_blob_ref();
CREATE TRIGGER node_attachments_blob_ref_del AFTER DELETE ON node_attachments
FOR EACH ROW EXECUTE FUNCTION node_attachments_blob_ref();
CREATE TRIGGER node_attachments_blob_ref_upd AFTER UPDATE OF blob_hash ON node_attachments
FOR EACH ROW WHEN (OLD.blob_hash IS DISTINCT FROM NEW.blob_hash)
EXECUTE FUNCTION node_attachments_blob_ref();

-- An attachment upload names its document instead of a folder or a file to
-- replace. It cascades with the document, like the node an upload replaces.
-- attachment_hash is what the committed upload became, so a client that lost
-- the final response learns it from HEAD instead of uploading again.
ALTER TABLE uploads ADD COLUMN attach_to UUID REFERENCES nodes(id) ON DELETE CASCADE;
ALTER TABLE uploads ADD COLUMN attachment_hash TEXT;
ALTER TABLE uploads ADD CONSTRAINT uploads_one_target
    CHECK (node_id IS NULL OR attach_to IS NULL);
