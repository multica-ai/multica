DROP TABLE IF EXISTS cerebro_round_cycle_item;
DROP TABLE IF EXISTS cerebro_round_cycle;

ALTER TABLE cerebro_round
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'batch' CHECK (mode IN ('live', 'batch')),
    ADD COLUMN schedule_cron TEXT,
    ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC',
    ADD COLUMN next_run_at TIMESTAMPTZ,
    ADD COLUMN cycle_opened_at TIMESTAMPTZ;

-- Rollback restores the former schema, not deleted held/retry/run history.
CREATE TABLE cerebro_round_held_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL DEFAULT 'assignee' CHECK (target_type IN ('assignee','agent','squad')),
    target_id UUID,
    state TEXT NOT NULL DEFAULT 'held' CHECK (state IN ('held','released','cancelled','retry','failed')),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count BETWEEN 0 AND 3),
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (round_id, comment_id, target_type, target_id)
);

CREATE TABLE cerebro_round_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','ready','completed','failed')),
    total_count INTEGER NOT NULL DEFAULT 0,
    responded_count INTEGER NOT NULL DEFAULT 0,
    stalled_count INTEGER NOT NULL DEFAULT 0,
    nudged_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX cerebro_round_one_active_run
    ON cerebro_round_run(round_id) WHERE status IN ('running','ready');

CREATE TABLE cerebro_round_run_item (
    run_id UUID NOT NULL REFERENCES cerebro_round_run(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    target_type TEXT NOT NULL DEFAULT 'assignee' CHECK (target_type IN ('assignee','agent','squad')),
    target_id UUID,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','responded','failed','stalled')),
    trigger_id UUID REFERENCES cerebro_round_held_trigger(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (run_id, issue_id, target_type, target_id)
);

CREATE INDEX cerebro_round_due_idx ON cerebro_round(next_run_at) WHERE next_run_at IS NOT NULL;
CREATE INDEX cerebro_round_held_idx ON cerebro_round_held_trigger(round_id, state, created_at);
CREATE INDEX cerebro_round_run_task_idx ON cerebro_round_run_item(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX cerebro_round_run_item_trigger_idx ON cerebro_round_run_item(trigger_id) WHERE trigger_id IS NOT NULL;
