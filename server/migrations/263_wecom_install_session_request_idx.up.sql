CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_request
    ON wecom_install_session (workspace_id, initiator_user_id, request_key_hash);
