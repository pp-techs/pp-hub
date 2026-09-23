-- Profile fields the identity provider owns, mirrored on sign-in and on an
-- organization reconcile, the same way email and name already are.
--
-- phone and gender are read-only here: the IdP is where they are edited.
--
-- The avatar is not a new column. The IdP's `picture` is fetched into the CAS
-- and served from the existing avatar route, so the Content-Security-Policy
-- keeps img-src to 'self' and an IdP on an internal hostname still works.
-- idp_picture is the source URL last seen, compared to decide whether to fetch
-- again; NULL means "never observed", which is what lets an upload made before
-- this migration survive the first sign-in after it. avatar_from_idp records
-- whose the current avatar is, so an IdP that drops the picture clears only an
-- avatar it supplied and never one the person uploaded.
ALTER TABLE users
    ADD COLUMN phone           TEXT NOT NULL DEFAULT '',
    ADD COLUMN gender          TEXT NOT NULL DEFAULT '',
    ADD COLUMN idp_picture     TEXT,
    ADD COLUMN avatar_from_idp BOOLEAN NOT NULL DEFAULT false;
