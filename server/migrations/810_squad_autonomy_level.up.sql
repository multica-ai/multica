-- The squad's autonomy level (spec 4.3): 0 Suggest, 1 Work, 2 Trusted,
-- 3 Autonomous. Existing squads default to 1 (Work), which is what they did
-- before levels existed. A constant default is a metadata change, so the
-- column is added without rewriting the table.
--
-- The CHECK is added NOT VALID and validated after, as in 782, so the table is
-- checked without holding a long lock.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS autonomy_level INTEGER NOT NULL DEFAULT 1;
ALTER TABLE squad DROP CONSTRAINT IF EXISTS squad_autonomy_level_check;
ALTER TABLE squad ADD CONSTRAINT squad_autonomy_level_check
    CHECK (autonomy_level BETWEEN 0 AND 3) NOT VALID;
ALTER TABLE squad VALIDATE CONSTRAINT squad_autonomy_level_check;
