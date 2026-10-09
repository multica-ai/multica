-- Lifecycle convergence is forward-only; never reopen legacy writes.
DO $$ BEGIN RAISE EXCEPTION 'MUL-7365 is forward-only; apply a corrective migration'; END $$;
