-- M5: background jobs + previews. See docs/architecture/milestones.md.

CREATE TYPE job_status AS ENUM ('pending', 'running', 'done', 'failed');

CREATE TABLE jobs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    kind        TEXT NOT NULL,               -- thumbnail | pdf | video | text | quota | gc
    payload     JSONB NOT NULL DEFAULT '{}',
    status      job_status NOT NULL DEFAULT 'pending',
    run_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX jobs_claim ON jobs (status, run_at) WHERE status = 'pending';

CREATE TABLE previews (
    node_version_id UUID NOT NULL REFERENCES node_versions(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,           -- thumbnail | pdf | video | text
    blob_hash       TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_version_id, kind)
);
