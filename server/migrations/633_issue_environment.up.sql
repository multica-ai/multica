-- Local environments (dev servers) a daemon started or detected for one of an
-- issue's tasks. The daemon reports a full snapshot per task; an environment it
-- stops reporting is ended rather than deleted, so the issue sidebar can show
-- what ran recently.
CREATE TABLE IF NOT EXISTS issue_environment (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id     UUID NOT NULL,
    task_id      UUID NOT NULL,
    runtime_id   UUID,
    agent_id     UUID,
    local_key    TEXT NOT NULL,
    source       TEXT NOT NULL CHECK (source IN ('config', 'detected')),
    name         TEXT NOT NULL,
    label        TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    port         INTEGER,
    state        TEXT NOT NULL CHECK (state IN ('starting', 'running', 'exited', 'stopped')),
    detail       TEXT NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at     TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
