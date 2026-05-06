-- Time entry table for live timer tracking (Toggl-style).
-- Running convention: stop_time = NULL, duration_seconds = -start_time.Unix() (large negative).
-- Stopped convention: stop_time set, duration_seconds = positive elapsed seconds.
CREATE TABLE time_entry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    description TEXT,
    start_time TIMESTAMPTZ NOT NULL,
    stop_time TIMESTAMPTZ,
    -- Negative while running (= -start_time.Unix()), positive when stopped (elapsed seconds).
    duration_seconds BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Primary access pattern: list all entries for a user in a workspace, newest first.
CREATE INDEX idx_time_entry_workspace_user ON time_entry (workspace_id, user_id, start_time DESC);
-- Secondary access pattern: list entries linked to an issue.
CREATE INDEX idx_time_entry_issue ON time_entry (issue_id) WHERE issue_id IS NOT NULL;

-- Materialised index for O(1) running-timer lookups.
-- One row per user; UPSERT on start, DELETE on stop/delete.
CREATE TABLE running_timer (
    user_id UUID PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE,
    time_entry_id UUID NOT NULL REFERENCES time_entry(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
