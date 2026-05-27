-- Cerebro workflow v2a: reusable status models (FIR-1550).
--
-- A status model is a workspace-level, reusable status pipeline that a project
-- can opt into. Each model owns an ordered list of custom statuses; every
-- custom status is bound to one of the 7 upstream base statuses (Jesper's
-- locked decision, FIR-1550) so reporting, inbox, and agents keep working
-- unchanged — the model only renames / recolors / reorders what the user sees.
--
--   cerebro_status_model          — one row per reusable model. The ordered
--                                   status list lives in the statuses JSONB
--                                   array: [{key,label,color,base_status,
--                                   position}]. Validation (>=3 statuses, each
--                                   base_status one of the 7 known values) is
--                                   enforced in the Go handler, not the DB, so
--                                   the upstream IssueStatus enum stays the
--                                   single source of truth for base values.
--   cerebro_project_status_model  — which model a project uses. PK on
--                                   project_id enforces "a project chooses one
--                                   model". Absence of a row = project uses the
--                                   default 7 statuses.
--
-- All idempotent (CREATE IF NOT EXISTS) so TestMigrationsAreIdempotent's
-- re-run pass is a no-op. Feature-flagged at runtime via cerebro_workflows;
-- tables stay empty until an admin creates a model.

CREATE TABLE IF NOT EXISTS cerebro_status_model (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT,

    -- Ordered list of custom statuses. Each element:
    --   { "key": "plan", "label": "Plan", "color": "#8b5cf6",
    --     "base_status": "todo", "position": 0 }
    statuses        JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_by_id   UUID NOT NULL,
    created_by_type TEXT NOT NULL
        CHECK (created_by_type IN ('member', 'agent')),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_status_model_workspace
    ON cerebro_status_model (workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_project_status_model (
    project_id      UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    status_model_id UUID NOT NULL REFERENCES cerebro_status_model(id) ON DELETE CASCADE,

    assigned_by_id   UUID NOT NULL,
    assigned_by_type TEXT NOT NULL
        CHECK (assigned_by_type IN ('member', 'agent')),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Supports the overview panel ("which projects use model X") and the
-- block-delete-if-in-use check, both keyed by status_model_id.
CREATE INDEX IF NOT EXISTS idx_cerebro_project_status_model_model
    ON cerebro_project_status_model (status_model_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_project_status_model_workspace
    ON cerebro_project_status_model (workspace_id);
