ALTER TABLE issue_workflow
    ADD COLUMN initial_status_id UUID;

ALTER TABLE issue_workflow_status
    ADD COLUMN spec_key TEXT;

UPDATE issue_workflow_status
SET spec_key = COALESCE(legacy_status_key, 'status_' || replace(id::text, '-', '_'))
WHERE spec_key IS NULL;

ALTER TABLE issue_workflow_status
    ALTER COLUMN spec_key SET DEFAULT ('status_' || replace(gen_random_uuid()::text, '-', '_')),
    ALTER COLUMN spec_key SET NOT NULL,
    ADD CONSTRAINT issue_workflow_status_spec_key_format
        CHECK (spec_key ~ '^[a-z][a-z0-9_]{0,63}$');

UPDATE issue_workflow AS workflow
SET initial_status_id = (
    SELECT status.id
    FROM issue_workflow_status AS status
    WHERE status.workflow_id = workflow.id
      AND status.workspace_id = workflow.workspace_id
      AND status.archived_at IS NULL
    ORDER BY
        CASE status.legacy_status_key
            WHEN 'todo' THEN 0
            WHEN 'backlog' THEN 1
            ELSE 2
        END,
        status.position,
        status.created_at,
        status.id
    LIMIT 1
)
WHERE workflow.initial_status_id IS NULL;
