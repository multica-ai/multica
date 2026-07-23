-- Initiative task: one node of an initiative's plan DAG. Rows are the plan —
-- they exist from planner acceptance, before any issue is created; `issue_id`
-- is stamped at dispatch (the linked issue carries origin_type='initiative',
-- origin_id=<this row's id> for crash-recovery adoption). `depends_on` holds
-- initiative_task ids; acyclicity is validated at plan acceptance in app code.
-- `task_key` is the planner's stable short key (e.g. "t1") used for dependency
-- resolution in planner output and plan-version diffs. `state` is the
-- orchestration overlay; the linked issue keeps its normal status so the board
-- stays truthful. `attempt` counts reconciler-level dispatch cycles,
-- independent of agent_task_queue's internal retry chain.
CREATE TABLE initiative_task (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    initiative_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    plan_version INTEGER NOT NULL,
    task_key TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('implement', 'research', 'review', 'integrate', 'docs', 'test')),
    depends_on UUID[] NOT NULL DEFAULT '{}',
    acceptance_criteria JSONB NOT NULL DEFAULT '[]',
    required_capabilities TEXT[] NOT NULL DEFAULT '{}',
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'ready', 'dispatched', 'in_progress', 'review', 'verifying', 'done', 'blocked', 'failed', 'retrying')),
    state_reason TEXT,
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER CHECK (max_attempts > 0),
    assignee_hint JSONB NOT NULL DEFAULT '{}',
    issue_id UUID,
    branch TEXT,
    stall_strikes INTEGER NOT NULL DEFAULT 0,
    last_activity_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
