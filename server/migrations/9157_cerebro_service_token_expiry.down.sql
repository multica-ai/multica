ALTER TABLE cerebro_service_token
    DROP CONSTRAINT IF EXISTS cerebro_service_token_read_only_scopes,
    DROP CONSTRAINT IF EXISTS cerebro_service_token_expiry_after_creation,
    ALTER COLUMN expires_at DROP NOT NULL;
