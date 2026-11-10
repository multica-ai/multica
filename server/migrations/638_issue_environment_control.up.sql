-- Controlling an issue's local environments from the sidebar (MUL-8923).
--
-- repo is the checkout directory a declared environment runs from, which
-- tells apart two repositories in one run that declare the same name.
-- excerpt holds the last redacted lines a crashed environment printed, so the
-- sidebar can say why it crashed while its machine is offline. controllable
-- marks rows reported by a daemon that accepts the commands below; rows from
-- older daemons stay read-only.
ALTER TABLE issue_environment
    ADD COLUMN IF NOT EXISTS repo TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS excerpt TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS controllable BOOLEAN NOT NULL DEFAULT false;

-- A stop, restart, start or log read a member asked a daemon to perform. The
-- daemon claims pending rows for its runtime and reports the outcome; control
-- rows double as the record of who did what. Log rows carry the output they
-- read and are deleted shortly after.
CREATE TABLE IF NOT EXISTS issue_environment_command (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL,
    issue_id       UUID NOT NULL,
    runtime_id     UUID NOT NULL,
    environment_id UUID,
    task_id        UUID,
    action         TEXT NOT NULL CHECK (action IN ('stop', 'restart', 'start', 'logs')),
    local_key      TEXT NOT NULL DEFAULT '',
    repo           TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    actor_type     TEXT NOT NULL,
    actor_id       UUID,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'succeeded', 'failed')),
    error          TEXT NOT NULL DEFAULT '',
    output         TEXT,
    result_key     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ
);

-- The environments a task's repositories declare, as the daemon found them
-- when the task finished, so the sidebar can offer to start them.
CREATE TABLE IF NOT EXISTS issue_environment_config (
    task_id      UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    issue_id     UUID NOT NULL,
    runtime_id   UUID NOT NULL,
    environments JSONB NOT NULL DEFAULT '[]',
    reported_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
