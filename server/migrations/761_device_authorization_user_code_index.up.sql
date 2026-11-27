CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_device_authorization_user_code ON device_authorization(user_code_hash) WHERE status = 'pending';
