CREATE TABLE project_space_import (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    project_id      UUID NOT NULL,
    batch_name      TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'uploading', 'finalizing', 'completed', 'partial', 'failed')),
    total_files     INTEGER NOT NULL,
    total_bytes     BIGINT NOT NULL,
    completed_files INTEGER NOT NULL DEFAULT 0,
    failed_files    INTEGER NOT NULL DEFAULT 0,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE TABLE project_space_import_file (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_id            UUID NOT NULL,
    workspace_id         UUID NOT NULL,
    project_id           UUID NOT NULL,
    relative_path        TEXT NOT NULL,
    stored_relative_path TEXT,
    content_type         TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes           BIGINT NOT NULL,
    sha256               TEXT,
    status               TEXT NOT NULL DEFAULT 'queued'
                         CHECK (status IN ('queued', 'uploading', 'completed', 'skipped', 'failed')),
    error_code           TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
