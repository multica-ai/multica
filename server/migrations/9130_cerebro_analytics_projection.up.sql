-- Cerebro-owned, workspace-scoped analytics projection for FIR-2996.
-- Source rows remain authoritative; these tables are rebuilt by the projector.

CREATE TABLE IF NOT EXISTS cerebro_analytics_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    population TEXT NOT NULL CHECK (population IN ('agent', 'gateway')),
    source_type TEXT NOT NULL CHECK (source_type IN ('dm', 'channel', 'issue', 'chat', 'autopilot', 'manual', 'gateway')),
    source_id UUID,
    source_label TEXT,
    person_id UUID,
    person_label TEXT,
    agent_id UUID,
    agent_label TEXT,
    project_id UUID,
    project_label TEXT,
    runtime_id UUID,
    runtime_label TEXT,
    provider TEXT,
    model TEXT,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    duration_seconds BIGINT CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    cost_cents BIGINT,
    cost_kind TEXT CHECK (cost_kind IS NULL OR cost_kind IN ('actual', 'calculated', 'missing')),
    trace_id TEXT,
    projected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, run_id)
);

CREATE TABLE IF NOT EXISTS cerebro_analytics_reference (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    analytics_run_id UUID NOT NULL REFERENCES cerebro_analytics_run(id) ON DELETE CASCADE,
    reference_kind TEXT NOT NULL CHECK (reference_kind IN ('issue', 'comment', 'dm', 'message', 'channel', 'chat', 'session', 'trace')),
    reference_id TEXT NOT NULL,
    label TEXT,
    href TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (analytics_run_id, reference_kind, reference_id)
);

CREATE TABLE IF NOT EXISTS cerebro_analytics_run_skill (
    analytics_run_id UUID NOT NULL REFERENCES cerebro_analytics_run(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    skill_id UUID,
    skill_name TEXT NOT NULL,
    invocation_count INTEGER NOT NULL CHECK (invocation_count > 0),
    first_used_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (analytics_run_id, skill_name)
);

CREATE TABLE IF NOT EXISTS cerebro_analytics_run_saving (
    analytics_run_id UUID NOT NULL REFERENCES cerebro_analytics_run(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    saving_type TEXT NOT NULL,
    mode TEXT NOT NULL,
    applied BOOLEAN NOT NULL,
    metric TEXT NOT NULL,
    baseline_value BIGINT NOT NULL,
    effective_value BIGINT NOT NULL,
    saved_tokens BIGINT NOT NULL DEFAULT 0,
    saved_cents BIGINT NOT NULL DEFAULT 0,
    measured_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (analytics_run_id, saving_type)
);

CREATE TABLE IF NOT EXISTS cerebro_analytics_quality_measurement (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    analytics_run_id UUID NOT NULL REFERENCES cerebro_analytics_run(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    measurement_type TEXT NOT NULL CHECK (measurement_type IN ('judge_gate', 'skill_observation', 'evaluator')),
    category TEXT NOT NULL,
    verdict TEXT NOT NULL,
    score DOUBLE PRECISION CHECK (score IS NULL OR (score >= 0 AND score <= 1)),
    confidence DOUBLE PRECISION CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    evidence_reference_id UUID REFERENCES cerebro_analytics_reference(id) ON DELETE SET NULL,
    evaluator_version TEXT,
    measured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (analytics_run_id, measurement_type, category, evaluator_version)
);

CREATE TABLE IF NOT EXISTS cerebro_analytics_visual (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    visual_type TEXT NOT NULL,
    query JSONB NOT NULL,
    display JSONB NOT NULL DEFAULT '{}',
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_workspace_time
    ON cerebro_analytics_run (workspace_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_source
    ON cerebro_analytics_run (workspace_id, source_type, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_person
    ON cerebro_analytics_run (workspace_id, person_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_project
    ON cerebro_analytics_run (workspace_id, project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_provider_model
    ON cerebro_analytics_run (workspace_id, provider, model, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_run_skill
    ON cerebro_analytics_run_skill (workspace_id, skill_name, last_used_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_quality
    ON cerebro_analytics_quality_measurement (workspace_id, measurement_type, category, measured_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_reference_lookup
    ON cerebro_analytics_reference (workspace_id, reference_kind, reference_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_analytics_visual_workspace_position
    ON cerebro_analytics_visual (workspace_id, position, created_at);

