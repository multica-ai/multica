-- Initiative: a natural-language idea the orchestrator turns into a planned DAG
-- of agent-executed tasks (Initiatives & Orchestrator RFC). `status` is the
-- initiative state machine; transitions are CAS-guarded in
-- server/internal/service/initiative_transitions.go, never enforced DB-side
-- beyond the enum CHECK. autonomy/budget/timeout columns are per-initiative
-- overrides — NULL means "inherit the workspace policy default".
-- pause_prev_status remembers where `paused` resumes to.
CREATE TABLE initiative (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    title TEXT NOT NULL,
    idea TEXT NOT NULL,
    constraints JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'planning', 'plan_review', 'executing', 'integrating', 'verifying', 'done', 'needs_human', 'paused', 'cancelled', 'failed')),
    autonomy_level SMALLINT CHECK (autonomy_level BETWEEN 1 AND 3),
    plan_version INTEGER NOT NULL DEFAULT 0,
    orchestrator_agent_id UUID,
    budget_limit_tokens BIGINT CHECK (budget_limit_tokens > 0),
    budget_spent_tokens BIGINT NOT NULL DEFAULT 0,
    max_parallel_tasks INTEGER CHECK (max_parallel_tasks > 0),
    max_attempts INTEGER CHECK (max_attempts > 0),
    stall_timeout_seconds INTEGER CHECK (stall_timeout_seconds > 0),
    external_wait_timeout_seconds INTEGER CHECK (external_wait_timeout_seconds > 0),
    pause_prev_status TEXT,
    pause_reason TEXT,
    needs_human_reason TEXT,
    created_by UUID NOT NULL,
    approved_by UUID,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
