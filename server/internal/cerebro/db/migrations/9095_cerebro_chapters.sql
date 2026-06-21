CREATE TABLE IF NOT EXISTS cerebro_chapters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('todo', 'in_progress', 'done')),
    position INT NOT NULL DEFAULT 0,
    handoff_summary TEXT NOT NULL DEFAULT '',
    handoff_done JSONB NOT NULL DEFAULT '[]'::jsonb,
    handoff_remaining JSONB NOT NULL DEFAULT '[]'::jsonb,
    plan_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS chapter_id UUID REFERENCES cerebro_chapters(id) ON DELETE SET NULL;
