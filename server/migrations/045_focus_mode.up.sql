CREATE TABLE IF NOT EXISTS focus_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('pomodoro', 'flowtime', 'quick_start')),
    phase TEXT NOT NULL CHECK (phase IN ('idle', 'focusing', 'paused', 'break_suggested', 'breaking', 'completed', 'abandoned')),
    preset TEXT,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    description TEXT,
    commitment_text TEXT,
    label_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    first_started_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    paused_at TIMESTAMPTZ,
    elapsed_focus_seconds INT NOT NULL DEFAULT 0,
    suggested_break_seconds INT,
    status_reason TEXT,
    reason_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, workspace_id)
);

CREATE INDEX idx_focus_sessions_workspace_user ON focus_sessions (workspace_id, user_id);
CREATE INDEX idx_focus_sessions_workspace_phase ON focus_sessions (workspace_id, phase);

CREATE TABLE IF NOT EXISTS focus_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    focus_session_id UUID REFERENCES focus_sessions(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    reason TEXT,
    note TEXT,
    duration_seconds INT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_focus_events_session ON focus_events (focus_session_id, created_at DESC);
CREATE INDEX idx_focus_events_workspace_user ON focus_events (workspace_id, user_id, created_at DESC);
