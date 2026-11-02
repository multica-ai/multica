-- Dropping a constraint drops the index it took over, so 701/702's down
-- directions are no-ops via IF EXISTS.
ALTER TABLE squad_routine DROP CONSTRAINT IF EXISTS squad_routine_pkey;
ALTER TABLE squad_charter DROP CONSTRAINT IF EXISTS squad_charter_pkey;
