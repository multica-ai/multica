ALTER TABLE channel_installation
    DROP CONSTRAINT IF EXISTS channel_installation_status_check,
    ADD CONSTRAINT channel_installation_status_check
        CHECK (status IN ('active', 'revoked', 'needs_reauth'));

COMMENT ON COLUMN channel_installation.status IS
    'active = supervised normally; revoked = user disconnected; needs_reauth = stored integration credentials could not be used after restart and must be refreshed.';
