-- FIR-3172: immutable mini-app bundles and per-version runtime deployments.

CREATE TABLE IF NOT EXISTS cerebro_app_version_file (
    app_id UUID NOT NULL,
    version TEXT NOT NULL,
    path TEXT NOT NULL,
    media_type TEXT NOT NULL,
    content BYTEA NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    PRIMARY KEY (app_id, version, path),
    FOREIGN KEY (app_id, version)
        REFERENCES cerebro_app_version(app_id, version) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cerebro_app_deployment (
    app_id UUID NOT NULL,
    version TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('docker', 'sliplane')),
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'provisioning', 'ready', 'failed', 'paused', 'deleting')),
    bundle_sha256 TEXT NOT NULL,
    external_service_id TEXT,
    internal_domain TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (app_id, version),
    FOREIGN KEY (app_id, version)
        REFERENCES cerebro_app_version(app_id, version) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_deployment_status
    ON cerebro_app_deployment(status, updated_at);
