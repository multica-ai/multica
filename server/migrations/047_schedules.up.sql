CREATE TABLE schedule (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    workflow_id   UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    schedule_type TEXT NOT NULL CHECK (schedule_type IN ('once', 'recurring')),
    cron_expr     TEXT,
    once_at       TIMESTAMPTZ,
    timezone      TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    weekdays      INT[] NOT NULL DEFAULT '{}',
    time_of_day   TIME NOT NULL DEFAULT '09:00:00',
    next_run_at   TIMESTAMPTZ NOT NULL,
    last_run_at   TIMESTAMPTZ,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_by    UUID NOT NULL REFERENCES "user"(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_schedule_workspace ON schedule(workspace_id);
CREATE INDEX idx_schedule_next_run ON schedule(next_run_at) WHERE enabled = true;
