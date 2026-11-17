CREATE INDEX CONCURRENTLY IF NOT EXISTS weixin_login_expiry_idx ON weixin_login_session (expires_at);
