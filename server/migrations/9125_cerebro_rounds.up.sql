CREATE TABLE IF NOT EXISTS cerebro_round (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    schedule_cron TEXT,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, owner_id, name)
);

CREATE TABLE IF NOT EXISTS cerebro_round_member (
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    added_by_type TEXT NOT NULL CHECK (added_by_type IN ('member', 'agent')),
    added_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (round_id, issue_id),
    UNIQUE (issue_id)
);

CREATE TABLE IF NOT EXISTS cerebro_round_held_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL DEFAULT 'assignee' CHECK (target_type IN ('assignee','agent','squad')),
    target_id UUID,
    state TEXT NOT NULL DEFAULT 'held' CHECK (state IN ('held', 'released', 'cancelled')),
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (round_id, comment_id, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS cerebro_round_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'ready', 'completed', 'failed')),
    total_count INTEGER NOT NULL DEFAULT 0,
    responded_count INTEGER NOT NULL DEFAULT 0,
    stalled_count INTEGER NOT NULL DEFAULT 0,
    nudged_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_round_one_active_run
    ON cerebro_round_run(round_id) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS cerebro_round_run_item (
    run_id UUID NOT NULL REFERENCES cerebro_round_run(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    target_type TEXT NOT NULL DEFAULT 'assignee' CHECK (target_type IN ('assignee','agent','squad')),
    target_id UUID,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'responded', 'failed', 'stalled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (run_id, issue_id, target_type, target_id)
);

CREATE INDEX IF NOT EXISTS cerebro_round_due_idx ON cerebro_round(next_run_at) WHERE next_run_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS cerebro_round_held_idx ON cerebro_round_held_trigger(round_id, state, created_at);
CREATE INDEX IF NOT EXISTS cerebro_round_run_task_idx ON cerebro_round_run_item(task_id) WHERE task_id IS NOT NULL;
