-- Avatar bytes go in the CAS like all other content; these two columns are the
-- reference the serving route resolves. avatar_url stays what it was — the
-- rendered URL every DTO already copies through — so nothing downstream
-- changes shape, and a cache-busting suffix can live in it.
ALTER TABLE users
    ADD COLUMN avatar_blob TEXT NOT NULL DEFAULT '',
    ADD COLUMN avatar_mime TEXT NOT NULL DEFAULT '';
