-- Skill Usage Tracking: record skill reads from task messages for async
-- statistics (Plan B — pure server-side, no daemon changes).

CREATE TABLE skill_usage_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, task_id)
);

CREATE INDEX idx_skill_usage_event_skill ON skill_usage_event(skill_id);
CREATE INDEX idx_skill_usage_event_issue ON skill_usage_event(issue_id);

-- Singleton cursor row for the async processor. last_task_id is NULL until
-- the first batch runs, meaning "start from the oldest completed task".
CREATE TABLE skill_usage_process_cursor (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_task_id UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO skill_usage_process_cursor (id, last_task_id) VALUES (1, NULL) ON CONFLICT DO NOTHING;
