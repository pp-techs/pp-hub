-- Space avatars, in the CAS like every other stored byte (0004 did this for
-- users). These two columns are the reference the serving route resolves;
-- there is no spaces.avatar_url column — the rendered URL is derived from the
-- hash at DTO time, so it can never go stale. '' means "no avatar" and is
-- never a reference.
ALTER TABLE spaces
    ADD COLUMN avatar_blob TEXT NOT NULL DEFAULT '',
    ADD COLUMN avatar_mime TEXT NOT NULL DEFAULT '';

CREATE INDEX spaces_avatar_blob ON spaces (avatar_blob) WHERE avatar_blob <> '';

-- Same shape as users_avatar_blob_ref (0011): the count moves in the same
-- statement as the reference, cascades included.
CREATE FUNCTION spaces_avatar_blob_ref() RETURNS trigger
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

CREATE TRIGGER spaces_avatar_blob_ref_ins AFTER INSERT ON spaces
FOR EACH ROW EXECUTE FUNCTION spaces_avatar_blob_ref();
CREATE TRIGGER spaces_avatar_blob_ref_del AFTER DELETE ON spaces
FOR EACH ROW EXECUTE FUNCTION spaces_avatar_blob_ref();
CREATE TRIGGER spaces_avatar_blob_ref_upd AFTER UPDATE OF avatar_blob ON spaces
FOR EACH ROW WHEN (OLD.avatar_blob IS DISTINCT FROM NEW.avatar_blob)
EXECUTE FUNCTION spaces_avatar_blob_ref();
