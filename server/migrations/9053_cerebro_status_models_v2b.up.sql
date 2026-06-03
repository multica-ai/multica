-- Cerebro workflow v2b: real sub-statuses (FIR-1550).
--
-- v2a let admins rename/recolor the 7 base statuses; that was rejected by
-- product because two custom statuses under the same base collapsed into a
-- single column and agents could not target a specific custom status.
--
-- v2b makes custom statuses first-class sub-statuses that live UNDER a base
-- status. The base status remains the upstream source of truth (so reports,
-- inbox, and any agent/automation built on the base enum keep working), and
-- a cerebro sidecar pins the specific custom status the issue actually
-- occupies.
--
-- Two changes:
--   1. statusEntry JSON shape (in cerebro_status_model.statuses) gains
--      "description" (text) and "triggers" (object). No schema change here —
--      JSONB stores the new fields directly. Validation lives in the Go
--      handler (description required when v2b is active; triggers fully
--      optional).
--   2. cerebro_issue_status — sidecar pointing an issue at one specific
--      custom status. issue_id is PK so a given issue has exactly one
--      pinned custom status (or none, in which case the project's default
--      mapping applies). base_status is denormalised so board queries that
--      group "all in-progress issues, by custom status" can answer without
--      joining the model JSONB.

CREATE TABLE IF NOT EXISTS cerebro_issue_status (
    issue_id          UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    status_model_id   UUID NOT NULL REFERENCES cerebro_status_model(id) ON DELETE CASCADE,
    custom_status_key TEXT NOT NULL,
    -- Denormalised: must equal the base_status of the statusEntry identified
    -- by (status_model_id, custom_status_key). Kept in sync by the Go handler.
    base_status       TEXT NOT NULL,

    set_by_id         UUID NOT NULL,
    set_by_type       TEXT NOT NULL CHECK (set_by_type IN ('member', 'agent', 'system')),

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Board grouping: "give me all issues in workspace W under model M grouped by
-- custom_status_key" hits this index.
CREATE INDEX IF NOT EXISTS idx_cerebro_issue_status_workspace_model
    ON cerebro_issue_status (workspace_id, status_model_id, custom_status_key);

-- Trigger engine lookup: "what was this issue's previous custom status" on
-- status change resolution is keyed by issue_id (the PK), so no extra index
-- needed there. But base-status filtering across a workspace's custom-mapped
-- issues benefits from a secondary index.
CREATE INDEX IF NOT EXISTS idx_cerebro_issue_status_base
    ON cerebro_issue_status (workspace_id, base_status);
