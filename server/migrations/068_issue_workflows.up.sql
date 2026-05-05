CREATE TABLE issue_workflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    is_default BOOLEAN NOT NULL DEFAULT false,
    selector JSONB NOT NULL DEFAULT '{}',
    allow_freeform BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (project_id IS NOT NULL OR is_default = true)
);

CREATE UNIQUE INDEX idx_issue_workflow_default_project
    ON issue_workflow(project_id)
    WHERE is_default AND project_id IS NOT NULL;

CREATE UNIQUE INDEX idx_issue_workflow_default_orphan
    ON issue_workflow(workspace_id)
    WHERE is_default AND project_id IS NULL;

CREATE UNIQUE INDEX idx_issue_workflow_project_name
    ON issue_workflow(project_id, lower(name))
    WHERE project_id IS NOT NULL;

CREATE TABLE issue_status_def (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES issue_workflow(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT NOT NULL CHECK (category IN ('backlog', 'unstarted', 'started', 'review', 'completed', 'cancelled', 'blocked')),
    color TEXT,
    icon TEXT,
    position FLOAT NOT NULL DEFAULT 0,
    on_main_graph BOOLEAN NOT NULL DEFAULT true,
    is_initial BOOLEAN NOT NULL DEFAULT false,
    is_terminal BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, key)
);

CREATE UNIQUE INDEX idx_issue_status_one_initial_per_workflow
    ON issue_status_def(workflow_id)
    WHERE is_initial;

CREATE TABLE issue_status_transition (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES issue_workflow(id) ON DELETE CASCADE,
    from_status_id UUID NOT NULL REFERENCES issue_status_def(id) ON DELETE CASCADE,
    to_status_id UUID NOT NULL REFERENCES issue_status_def(id) ON DELETE CASCADE,
    action_label TEXT,
    description TEXT,
    guard JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, from_status_id, to_status_id)
);

CREATE INDEX idx_issue_status_def_workflow_position ON issue_status_def(workflow_id, position);
CREATE INDEX idx_issue_status_transition_from ON issue_status_transition(workflow_id, from_status_id);

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

CREATE OR REPLACE FUNCTION seed_default_issue_workflow(workflow_uuid UUID)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    from_status UUID;
    to_status UUID;
    from_key TEXT;
    to_key TEXT;
    status_keys TEXT[] := ARRAY['backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'];
BEGIN
    INSERT INTO issue_status_def (workflow_id, key, name, description, category, position, on_main_graph, is_initial, is_terminal)
    VALUES
        (workflow_uuid, 'backlog', 'Backlog', 'Parked before the main workflow starts', 'backlog', 0, false, false, false),
        (workflow_uuid, 'todo', 'TODO', 'Ready to start', 'unstarted', 10, true, true, false),
        (workflow_uuid, 'in_progress', 'In progress', 'Work is underway', 'started', 20, true, false, false),
        (workflow_uuid, 'in_review', 'In review', 'Awaiting review', 'review', 30, true, false, false),
        (workflow_uuid, 'done', 'Done', 'Completed successfully', 'completed', 40, true, false, true),
        (workflow_uuid, 'blocked', 'Blocked', 'Stuck on an external factor', 'blocked', 50, false, false, false),
        (workflow_uuid, 'cancelled', 'Cancelled', 'Cancelled', 'cancelled', 60, false, false, true)
    ON CONFLICT (workflow_id, key) DO NOTHING;

    FOREACH from_key IN ARRAY status_keys LOOP
        SELECT id INTO from_status FROM issue_status_def WHERE workflow_id = workflow_uuid AND key = from_key;
        FOREACH to_key IN ARRAY status_keys LOOP
            IF from_key <> to_key THEN
                SELECT id INTO to_status FROM issue_status_def WHERE workflow_id = workflow_uuid AND key = to_key;
                INSERT INTO issue_status_transition (workflow_id, from_status_id, to_status_id, action_label)
                VALUES (workflow_uuid, from_status, to_status, 'Move to ' || to_key)
                ON CONFLICT DO NOTHING;
            END IF;
        END LOOP;
    END LOOP;
END $$;

DO $$
DECLARE
    ws RECORD;
    p RECORD;
    wf_id UUID;
BEGIN
    FOR ws IN SELECT id FROM workspace LOOP
        INSERT INTO issue_workflow (workspace_id, project_id, name, description, is_default)
        VALUES (ws.id, NULL, 'Default', 'Default workflow for projectless issues', true)
        ON CONFLICT DO NOTHING
        RETURNING id INTO wf_id;

        IF wf_id IS NULL THEN
            SELECT id INTO wf_id
            FROM issue_workflow
            WHERE workspace_id = ws.id AND project_id IS NULL AND is_default = true
            LIMIT 1;
        END IF;

        PERFORM seed_default_issue_workflow(wf_id);
    END LOOP;

    FOR p IN SELECT id, workspace_id FROM project LOOP
        wf_id := NULL;
        INSERT INTO issue_workflow (workspace_id, project_id, name, description, is_default)
        VALUES (p.workspace_id, p.id, 'Default', 'Default issue workflow', true)
        ON CONFLICT DO NOTHING
        RETURNING id INTO wf_id;

        IF wf_id IS NULL THEN
            SELECT id INTO wf_id
            FROM issue_workflow
            WHERE project_id = p.id AND is_default = true
            LIMIT 1;
        END IF;

        PERFORM seed_default_issue_workflow(wf_id);
    END LOOP;
END $$;

DROP FUNCTION seed_default_issue_workflow(UUID);
