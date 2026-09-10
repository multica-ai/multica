-- Additive Issue Lifecycle domain foundation (MUL-7022).
--
-- The legacy issue.status / issue_status catalog remains available throughout
-- rollout. New rows are deliberately not FK-backed: repository policy keeps
-- ownership and cleanup in the application layer so rolling binaries can use
-- the schema in either direction.
CREATE TABLE issue_lifecycle (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project')),
    scope_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE issue_lifecycle_status (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    lifecycle_id UUID NOT NULL,
    legacy_status_key TEXT,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 256),
    color TEXT NOT NULL CHECK (color ~ '^#[0-9a-f]{6}$'),
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    phase TEXT NOT NULL CHECK (phase IN ('backlog', 'unstarted', 'started', 'completed', 'cancelled')),
    outcome TEXT CHECK (outcome IS NULL OR outcome IN ('completed', 'cancelled')),
    entry_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    entry_policy_revision BIGINT NOT NULL DEFAULT 1 CHECK (entry_policy_revision > 0),
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_lifecycle_status_entry_policy_object
        CHECK (jsonb_typeof(entry_policy) = 'object'),
    CONSTRAINT issue_lifecycle_status_outcome_matches_phase
        CHECK (
            (phase = 'completed' AND outcome = 'completed') OR
            (phase = 'cancelled' AND outcome = 'cancelled') OR
            (phase NOT IN ('completed', 'cancelled') AND outcome IS NULL)
        )
);

CREATE TABLE issue_transition (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    lifecycle_id UUID NOT NULL,
    lifecycle_revision BIGINT NOT NULL CHECK (lifecycle_revision > 0),
    from_status_id UUID,
    to_status_id UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system', 'integration')),
    actor_id UUID,
    cause TEXT NOT NULL CHECK (char_length(cause) BETWEEN 1 AND 64),
    issue_revision_before BIGINT NOT NULL CHECK (issue_revision_before > 0),
    issue_revision_after BIGINT NOT NULL CHECK (issue_revision_after >= issue_revision_before),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_transition_principal_actor
        CHECK (actor_type NOT IN ('member', 'agent') OR actor_id IS NOT NULL)
);

CREATE TABLE automation_execution (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    trigger_transition_id UUID NOT NULL,
    lifecycle_id UUID NOT NULL,
    lifecycle_revision BIGINT NOT NULL CHECK (lifecycle_revision > 0),
    status_id UUID NOT NULL,
    policy_revision BIGINT NOT NULL CHECK (policy_revision > 0),
    policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    executor_type TEXT CHECK (executor_type IS NULL OR executor_type IN ('agent', 'squad', 'workflow')),
    executor_id UUID,
    status TEXT NOT NULL DEFAULT 'dormant' CHECK (
        status IN ('dormant', 'pending', 'queued', 'running', 'completed', 'failed', 'cancelled', 'superseded')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT automation_execution_policy_snapshot_object
        CHECK (jsonb_typeof(policy_snapshot) = 'object')
);

ALTER TABLE workspace ADD COLUMN default_issue_lifecycle_id UUID;
ALTER TABLE project ADD COLUMN default_issue_lifecycle_id UUID;
ALTER TABLE issue ADD COLUMN lifecycle_id UUID;
ALTER TABLE issue ADD COLUMN lifecycle_status_id UUID;
ALTER TABLE issue ADD COLUMN last_transition_id UUID;
ALTER TABLE agent_task_queue ADD COLUMN automation_execution_id UUID;
