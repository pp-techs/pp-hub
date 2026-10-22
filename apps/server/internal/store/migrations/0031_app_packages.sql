-- Packaged apps (M51, docs/architecture/extensions.md#packaged-apps): an
-- extension an instance administrator uploads as a .zip, which works without
-- a rebuild. No uploaded code runs in this process: the package is static web
-- files, shown in a sandboxed frame, and the data its nodes hold is the record
-- store below, guarded by the node's capabilities like everything else.

-- One row per installed package. The id shares the namespace of the built-in
-- extensions (ext_state, notification kinds, node kinds), which the host
-- checks at install time.
CREATE TABLE ext_packages (
    ext_id             TEXT PRIMARY KEY CHECK (ext_id ~ '^[a-z][a-z0-9_]{0,31}$'),
    -- pp-app.json as uploaded; the host re-parses it at startup.
    manifest           JSONB NOT NULL,
    version            TEXT NOT NULL,
    -- sha256 over the files (apppkg.Read): names the asset URL, so an upgrade
    -- is a new URL and every asset can be cached forever.
    digest             TEXT NOT NULL,
    size               BIGINT NOT NULL,
    -- The administrator's address for the app's backend, overriding the
    -- manifest's; NULL follows the manifest.
    backend_url        TEXT,
    -- The key the host signs backend tokens with, AES-GCM under
    -- DeriveKey("exthost/backend-secret"). NULL until an administrator
    -- generates one; the backend proxy refuses without it.
    backend_secret     TEXT,
    installed_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    installed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The package's files. Kept in the database rather than the blob store: a
-- package is small, is replaced whole on upgrade (one transaction), and
-- pg_dump already carries it.
CREATE TABLE ext_package_files (
    ext_id       TEXT NOT NULL REFERENCES ext_packages(ext_id) ON DELETE CASCADE,
    path         TEXT NOT NULL,
    content_type TEXT NOT NULL,
    sha256       TEXT NOT NULL,
    body         BYTEA NOT NULL,
    PRIMARY KEY (ext_id, path)
);

-- The record store of packaged apps' nodes. The key is (node, id) so a copy
-- of a node keeps its records' ids, and with them every reference one record
-- holds to another. Purging the node takes its records with it.
CREATE TABLE ext_records (
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    id         UUID NOT NULL DEFAULT gen_random_uuid(),
    collection TEXT NOT NULL CHECK (collection ~ '^[a-z][a-z0-9_]{0,31}$'),
    -- The API holds a record to 64 KiB as sent; this is the backstop for a
    -- merge, measured on jsonb's own (wider) text.
    data       JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object' AND octet_length(data::text) <= 131072),
    rev        BIGINT NOT NULL DEFAULT 1,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, id)
);
CREATE INDEX ext_records_by_collection ON ext_records (node_id, collection, created_at, id);
CREATE INDEX ext_records_by_time ON ext_records (node_id, created_at, id);
