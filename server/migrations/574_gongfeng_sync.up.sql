CREATE TABLE IF NOT EXISTS gongfeng_repository (
    connection_id UUID NOT NULL,
    project_id BIGINT NOT NULL,
    path TEXT NOT NULL,
    web_url TEXT NOT NULL,
    clone_url TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT '',
    hook_id BIGINT NOT NULL DEFAULT 0,
    hook_revision TIMESTAMPTZ,
    next_page INTEGER NOT NULL DEFAULT 1,
    scan_started_at TIMESTAMPTZ,
    synced_at TIMESTAMPTZ,
    attempted_at TIMESTAMPTZ,
    sync_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE IF EXISTS vcs_pull_request
    ADD COLUMN IF NOT EXISTS snapshot_head_sha TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS snapshot_fetched_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS snapshot JSONB,
    ADD COLUMN IF NOT EXISTS snapshot_error TEXT NOT NULL DEFAULT '';
