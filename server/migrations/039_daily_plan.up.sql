CREATE TABLE daily_plan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    plan_date DATE NOT NULL,
    draft_content TEXT NOT NULL DEFAULT '',
    top_issue_ids UUID[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    confirmed_at TIMESTAMPTZ,
    generated_by TEXT NOT NULL DEFAULT 'manual' CHECK (generated_by IN ('manual', 'scheduled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, plan_date)
);

CREATE INDEX idx_daily_plan_user ON daily_plan (workspace_id, user_id, plan_date DESC);
