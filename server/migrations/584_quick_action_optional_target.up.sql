ALTER TABLE quick_action
    DROP CONSTRAINT IF EXISTS quick_action_assignee_type_check,
    ALTER COLUMN assignee_id DROP NOT NULL,
    ADD CONSTRAINT quick_action_assignee_type_check CHECK (
        (assignee_type = '' AND assignee_id IS NULL) OR
        (assignee_type IN ('agent', 'squad') AND assignee_id IS NOT NULL)
    );
