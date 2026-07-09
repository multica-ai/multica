UPDATE channel_installation
SET status = 'revoked'
WHERE status = 'needs_reauth';

ALTER TABLE channel_installation
    DROP CONSTRAINT IF EXISTS channel_installation_status_check,
    ADD CONSTRAINT channel_installation_status_check
        CHECK (status IN ('active', 'revoked'));
