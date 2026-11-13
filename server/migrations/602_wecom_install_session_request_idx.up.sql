-- Makes begin idempotent per (workspace, initiator, idempotency key), so a
-- double-clicked button or a retried request reuses one session instead of
-- burning a second WeCom generate call and showing the admin a second QR.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_request
    ON wecom_install_session (workspace_id, initiator_user_id, request_key_hash);
