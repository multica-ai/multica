-- HCX briefly applied this object under legacy 229/230 filenames before the
-- upstream range was reconciled. Preserve it when either legacy ledger entry
-- owns the table.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM schema_migrations
        WHERE version IN (
            '229_control_plane_effect_ledger',
            '230_control_plane_effect_ledger'
        )
    ) THEN
        DROP TABLE IF EXISTS control_plane_effect_ledger;
    END IF;
END
$$;
