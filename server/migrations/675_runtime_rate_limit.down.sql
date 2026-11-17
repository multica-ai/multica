-- Drop the rate-limit table only when this filename is the ledger entry that
-- owns it. A database that already applied 665_runtime_rate_limit still has
-- that row, and there is no 665_runtime_rate_limit.down.sql left to run.
-- Dropping here would destroy snapshots while the old row still says the
-- migration is applied. Remove that legacy row first if the table has to go.
DO $$
BEGIN
  IF to_regclass('schema_migrations') IS NOT NULL AND EXISTS (
    SELECT 1 FROM schema_migrations WHERE version = '665_runtime_rate_limit'
  ) THEN
    RAISE NOTICE 'migration report: keeping runtime_rate_limit because 665_runtime_rate_limit is still recorded';
    RETURN;
  END IF;

  DROP TABLE IF EXISTS runtime_rate_limit;
END $$;
