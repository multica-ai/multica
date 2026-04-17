-- GitLab integration: per-workspace and per-user connection records.
-- Phase 1 only creates the tables + indices; webhook/sync columns are left
-- nullable because Phase 2 populates them during initial sync.

CREATE TABLE IF NOT EXISTS workspace_gitlab_connection (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_project_id BIGINT NOT NULL,
    gitlab_project_path TEXT NOT NULL,
    service_token_encrypted BYTEA NOT NULL,
    service_token_user_id BIGINT NOT NULL,
    webhook_secret TEXT,
    webhook_gitlab_id BIGINT,
    last_sync_cursor TIMESTAMPTZ,
    connection_status TEXT NOT NULL DEFAULT 'connected'
        CHECK (connection_status IN ('connecting', 'connected', 'error')),
    status_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_gitlab_connection_project
    ON workspace_gitlab_connection(gitlab_project_id);

CREATE TABLE IF NOT EXISTS user_gitlab_connection (
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_user_id BIGINT NOT NULL,
    gitlab_username TEXT NOT NULL,
    pat_encrypted BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);

CREATE INDEX IF NOT EXISTS idx_user_gitlab_connection_workspace
    ON user_gitlab_connection(workspace_id);
