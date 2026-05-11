-- Add type column to time_entries to distinguish entry sources.
ALTER TABLE time_entry ADD COLUMN type TEXT NOT NULL DEFAULT 'manual';

-- Store per-user Pomodoro session state for persistence across page reloads.
CREATE TABLE pomodoro_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    phase TEXT NOT NULL DEFAULT 'work',
    phase_duration_seconds INT NOT NULL DEFAULT 1500,
    status TEXT NOT NULL DEFAULT 'idle',
    elapsed_seconds INT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX pomodoro_sessions_user_workspace_idx
    ON pomodoro_sessions (user_id, workspace_id);
