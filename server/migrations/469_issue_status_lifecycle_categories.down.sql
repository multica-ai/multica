-- Consolidation deliberately discards historical behavior inheritance.
-- Deployment policy is fix-forward; do not manufacture a lossy inverse.
DO $$ BEGIN RAISE EXCEPTION 'MUL-7240 is forward-only; apply a corrective migration'; END $$;
