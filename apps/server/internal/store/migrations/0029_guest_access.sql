-- Guest access (docs/architecture/guest-access.md).
--
-- A guest is somebody outside the instance who proved they own an email
-- address with a one-time code and was shared something at that address. The
-- identity is a `users` row, so a grant, a comment and a notification need no
-- second kind of principal; what marks it is `kind`, which the HTTP edge
-- reads to confine the session to a short list of read-and-comment routes.
--
-- A guest has no identity-provider identity: oidc_iss is '' and oidc_sub is
-- 'guest:<id>', which keeps UNIQUE (oidc_iss, oidc_sub) without loosening the
-- NOT NULL. A later verified sign-in with the same address re-points the row
-- (AdoptUserIdentity), which is why it keeps its id, grants and comments.
ALTER TABLE users ADD COLUMN kind TEXT NOT NULL DEFAULT 'member'
    CONSTRAINT users_kind_ck CHECK (kind IN ('member', 'guest'));

ALTER TABLE users ADD CONSTRAINT users_guest_ck
    CHECK (kind <> 'guest' OR (oidc_iss = '' AND email <> '' AND email_verified));

-- One live code per address. `sends` and `window_start` bound how often a
-- code is mailed to one address; `attempts` bounds guesses against it. Only
-- the SHA-256 of the code is stored.
CREATE TABLE guest_otps (
    email_norm   TEXT PRIMARY KEY CHECK (email_norm = lower(btrim(email_norm))),
    code_hash    BYTEA       NOT NULL,
    attempts     INT         NOT NULL DEFAULT 0,
    sends        INT         NOT NULL DEFAULT 1,
    window_start TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX guest_otps_expires_at ON guest_otps (expires_at);
