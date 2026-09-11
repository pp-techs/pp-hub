-- Extend users table with profile fields and admin flag.
-- display_name: user-editable display name (distinct from OIDC name).
-- avatar_url: URL or blob path to user avatar.
-- bio: user bio/description.
-- settings: user preferences as JSONB.
-- is_admin: system admin flag (for admin-only endpoints).
-- last_seen: last OIDC login timestamp.

ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN settings JSONB NOT NULL DEFAULT '{}';
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN last_seen TIMESTAMPTZ;

-- email_hash for search: normalized lowercase email hash (sha256).
-- Unique per tenant to prevent email collisions.
-- Handle duplicates: keep the oldest row per email, null out email_hash for others.
ALTER TABLE users ADD COLUMN email_hash TEXT NOT NULL DEFAULT '';

-- Update existing rows with email_hash, keeping only the first occurrence per tenant+email
UPDATE users u SET email_hash = LOWER(ENCODE(SHA256(u.email::bytea), 'hex'))
WHERE u.id IN (
  SELECT DISTINCT ON (tenant_id, email) id
  FROM users
  ORDER BY tenant_id, email, created_at
);

-- Now create unique index (duplicates have empty email_hash, excluded by WHERE clause)
CREATE UNIQUE INDEX users_tenant_email_hash ON users (tenant_id, email_hash) WHERE email_hash != '';

-- Trigger to maintain email_hash on insert/update
CREATE OR REPLACE FUNCTION users_email_hash_trigger()
RETURNS TRIGGER AS $$
BEGIN
  NEW.email_hash := LOWER(ENCODE(SHA256(NEW.email::bytea), 'hex'));
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_email_hash
  BEFORE INSERT OR UPDATE OF email ON users
  FOR EACH ROW EXECUTE FUNCTION users_email_hash_trigger();
