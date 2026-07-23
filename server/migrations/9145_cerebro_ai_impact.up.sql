-- FIR-3411: workspace-scoped AI Impact taxonomy and append-only evidence.
-- No observations are seeded: production evidence must come from measured sources
-- or an explicitly audited manual entry.

CREATE TABLE IF NOT EXISTS cerebro_ai_impact_function (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    description TEXT NOT NULL DEFAULT '',
    owner_type TEXT NOT NULL CHECK (owner_type IN ('member', 'agent')),
    owner_id UUID NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    UNIQUE (workspace_id, id)
);

CREATE TABLE IF NOT EXISTS cerebro_ai_impact_operating_loop (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    function_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    description TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_ai_impact_loop_function_workspace_fk
        FOREIGN KEY (workspace_id, function_id)
        REFERENCES cerebro_ai_impact_function (workspace_id, id)
        ON DELETE CASCADE,
    UNIQUE (workspace_id, function_id, name),
    UNIQUE (workspace_id, id)
);

CREATE TABLE IF NOT EXISTS cerebro_ai_impact_project_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    operating_loop_id UUID NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_ai_impact_binding_loop_workspace_fk
        FOREIGN KEY (workspace_id, operating_loop_id)
        REFERENCES cerebro_ai_impact_operating_loop (workspace_id, id)
        ON DELETE CASCADE
);

CREATE OR REPLACE FUNCTION enforce_ai_impact_project_workspace() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM project
        WHERE id = NEW.project_id
          AND workspace_id = NEW.workspace_id
    ) THEN
        RAISE EXCEPTION 'AI Impact project binding must stay within one workspace';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS enforce_ai_impact_project_workspace_trigger
    ON cerebro_ai_impact_project_binding;
CREATE TRIGGER enforce_ai_impact_project_workspace_trigger
BEFORE INSERT OR UPDATE ON cerebro_ai_impact_project_binding
FOR EACH ROW EXECUTE FUNCTION enforce_ai_impact_project_workspace();

-- A Project has at most one active Operating Loop in v1. Historical inactive
-- bindings remain available for attribution audits.
CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_ai_impact_project_active_loop
    ON cerebro_ai_impact_project_binding (workspace_id, project_id)
    WHERE active;
CREATE INDEX IF NOT EXISTS idx_cerebro_ai_impact_binding_loop
    ON cerebro_ai_impact_project_binding (workspace_id, operating_loop_id)
    WHERE active;

CREATE TABLE IF NOT EXISTS cerebro_ai_impact_metric (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    operating_loop_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    family TEXT NOT NULL
        CHECK (family IN ('Adoption', 'Output', 'Outcome', 'Quality', 'Economics', 'Risk')),
    unit TEXT NOT NULL CHECK (btrim(unit) <> ''),
    direction TEXT NOT NULL CHECK (direction IN ('increase', 'decrease')),
    baseline_start TIMESTAMPTZ NOT NULL,
    baseline_end TIMESTAMPTZ NOT NULL,
    target_value DOUBLE PRECISION,
    source TEXT NOT NULL CHECK (btrim(source) <> ''),
    guardrail BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_ai_impact_metric_baseline_ordered
        CHECK (baseline_start < baseline_end),
    CONSTRAINT cerebro_ai_impact_metric_loop_workspace_fk
        FOREIGN KEY (workspace_id, operating_loop_id)
        REFERENCES cerebro_ai_impact_operating_loop (workspace_id, id)
        ON DELETE CASCADE,
    UNIQUE (workspace_id, operating_loop_id, name),
    UNIQUE (workspace_id, id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_ai_impact_metric_family
    ON cerebro_ai_impact_metric (workspace_id, family, active);
CREATE INDEX IF NOT EXISTS idx_cerebro_ai_impact_metric_loop
    ON cerebro_ai_impact_metric (workspace_id, operating_loop_id, active);

CREATE TABLE IF NOT EXISTS cerebro_ai_impact_observation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    metric_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    value DOUBLE PRECISION,
    evidence_status TEXT NOT NULL
        CHECK (evidence_status IN ('Measured', 'Estimated', 'Missing')),
    confidence DOUBLE PRECISION NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    source TEXT NOT NULL CHECK (btrim(source) <> ''),
    method TEXT NOT NULL CHECK (btrim(method) <> ''),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_ai_impact_observation_period_ordered
        CHECK (period_start < period_end),
    CONSTRAINT cerebro_ai_impact_observation_value_matches_evidence
        CHECK (
            (evidence_status = 'Missing' AND value IS NULL)
            OR (evidence_status IN ('Measured', 'Estimated') AND value IS NOT NULL)
        ),
    CONSTRAINT cerebro_ai_impact_observation_actor_valid
        CHECK (
            (created_by_type = 'system' AND created_by_id IS NULL)
            OR (created_by_type IN ('member', 'agent') AND created_by_id IS NOT NULL)
        ),
    CONSTRAINT cerebro_ai_impact_observation_metric_workspace_fk
        FOREIGN KEY (workspace_id, metric_id)
        REFERENCES cerebro_ai_impact_metric (workspace_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cerebro_ai_impact_observation_metric_period
    ON cerebro_ai_impact_observation (
        workspace_id,
        metric_id,
        period_start,
        period_end,
        created_at DESC
    );
CREATE INDEX IF NOT EXISTS idx_cerebro_ai_impact_observation_workspace_created
    ON cerebro_ai_impact_observation (workspace_id, created_at DESC);

CREATE OR REPLACE FUNCTION prevent_ai_impact_observation_mutation() RETURNS trigger AS $$
BEGIN
    -- Preserve normal workspace deletion while rejecting direct evidence edits
    -- and cascades from deleting a Metric that still has observations.
    IF TG_OP = 'DELETE' AND NOT EXISTS (
        SELECT 1 FROM workspace WHERE id = OLD.workspace_id
    ) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'AI Impact observations are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_ai_impact_observation_mutation_trigger
    ON cerebro_ai_impact_observation;
CREATE TRIGGER prevent_ai_impact_observation_mutation_trigger
BEFORE UPDATE OR DELETE ON cerebro_ai_impact_observation
FOR EACH ROW EXECUTE FUNCTION prevent_ai_impact_observation_mutation();
