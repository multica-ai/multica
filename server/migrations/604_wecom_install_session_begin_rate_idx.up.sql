-- Serves the per-workspace begin rate check, which counts recent sessions to
-- stop one workspace from spending the deployment's shared WeCom generate quota.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_begin_rate
    ON wecom_install_session (workspace_id, created_at, initiator_user_id);
