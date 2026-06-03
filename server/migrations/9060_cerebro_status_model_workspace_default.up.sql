-- Cerebro workflow v2b phase 2 (FIR-2800): workspace-level default status model.
--
-- An admin can mark one status model as the workspace default. New projects
-- created after the flag is set are automatically assigned that model via the
-- CEREBRO-PATCH(project-create-default-status-model) hook in
-- server/internal/handler/project.go, so the admin does not have to manually
-- assign the model to every new project.
--
-- Only one model per workspace may be the default at any time — the partial
-- unique index enforces this at the DB level, so a concurrent write cannot
-- accidentally create two defaults.

ALTER TABLE cerebro_status_model
    ADD COLUMN IF NOT EXISTS workspace_default BOOLEAN NOT NULL DEFAULT false;

-- Partial unique index: at most one row per workspace with workspace_default=true.
-- Using a partial index rather than a unique column keeps the "not default" path
-- (the common case) index-free.
CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_status_model_workspace_default
    ON cerebro_status_model (workspace_id)
    WHERE workspace_default = true;
