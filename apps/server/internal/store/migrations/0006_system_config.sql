-- System configuration key-value store for operational and policy knobs.
--
-- Knobs stored here take precedence over environment variable defaults,
-- allowing administrators to adjust policies (e.g. max upload size, audit retention)
-- without restarting containers or editing deployment files.

CREATE TABLE system_config (
    key         TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    value       JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_system_config_tenant ON system_config (tenant_id);
