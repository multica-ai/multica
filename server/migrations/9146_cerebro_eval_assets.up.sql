-- FIR-3496 (Evals v2, Fase 0): version families for in-app authored evals.
-- The 9140 catalog already carries datasets/graders/thresholds JSONB. This
-- migration groups the immutable per-version rows into an editable "family":
-- editing an active eval creates a NEW version row that shares eval_family_id,
-- while old cerebro_eval_run rows keep pointing at the exact version they ran.
-- Idempotent: safe to run against a catalog that predates the column.

ALTER TABLE cerebro_eval
    ADD COLUMN IF NOT EXISTS eval_family_id UUID;

-- Backfill: every existing version becomes the root of its own family so no
-- row is left without a family, and same-key versions collapse onto the
-- oldest row's id (the family root).
UPDATE cerebro_eval e
SET eval_family_id = root.id
FROM (
    SELECT DISTINCT ON (workspace_id, eval_key)
        workspace_id, eval_key, id
    FROM cerebro_eval
    ORDER BY workspace_id, eval_key, created_at ASC, id ASC
) root
WHERE e.eval_family_id IS NULL
  AND e.workspace_id = root.workspace_id
  AND e.eval_key = root.eval_key;

-- Any row still unmatched (shouldn't happen) roots its own family.
UPDATE cerebro_eval
SET eval_family_id = id
WHERE eval_family_id IS NULL;

ALTER TABLE cerebro_eval
    ALTER COLUMN eval_family_id SET DEFAULT gen_random_uuid();

CREATE INDEX IF NOT EXISTS idx_cerebro_eval_family
    ON cerebro_eval(workspace_id, eval_family_id, created_at DESC);
