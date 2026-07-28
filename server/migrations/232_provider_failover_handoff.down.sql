-- HCX briefly applied this object under the legacy 225 filename before the
-- upstream 225-231 range landed. Preserve the table when that older ledger
-- entry owns it; otherwise this is an ordinary clean-install rollback.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version = '225_provider_failover_handoff'
    ) THEN
        DROP TABLE IF EXISTS provider_failover_handoff;
    END IF;
END
$$;
