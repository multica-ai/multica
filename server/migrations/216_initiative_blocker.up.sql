-- Initiative blocker: a normalized, answerable question raised while executing
-- an initiative task. Created by the reconciler's blocker-intake step (issue
-- status 'blocked' or a terminal agent_blocked task failure); `category` stays
-- NULL until the triage decision classifies it. `resolution` records what the
-- orchestrator or human did (JSONB: action, answer, triage confidence, ...).
CREATE TABLE initiative_blocker (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    task_id UUID NOT NULL,
    source_comment_id UUID,
    category TEXT CHECK (category IN ('missing_context', 'bad_decomposition', 'external_dependency', 'env_or_runtime', 'decision_needed', 'agent_confusion')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'triaging', 'answered', 'resolved', 'dismissed')),
    question TEXT NOT NULL,
    resolution JSONB,
    answered_by UUID,
    answered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
