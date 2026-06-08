-- TECH-3077: Skill automation link table for impact analysis.
-- Records which autopilots/cron-jobs trigger each skill, and which skills
-- chain to/from each other. Synced from skill.metadata->triggered_by
-- on every create/update.

CREATE TABLE IF NOT EXISTS skill_automation_link (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    automation_type TEXT NOT NULL,  -- 'autopilot' | 'cron' | 'webhook'
    automation_id UUID,             -- optional: autopilot UUID
    automation_name TEXT NOT NULL,
    schedule TEXT,                  -- e.g. 'kl. 10:00 Europe/Copenhagen'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, automation_type, automation_name)
);

CREATE INDEX IF NOT EXISTS idx_skill_automation_link_skill
    ON skill_automation_link(skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_automation_link_automation_id
    ON skill_automation_link(automation_id) WHERE automation_id IS NOT NULL;
