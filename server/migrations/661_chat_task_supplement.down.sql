-- Drain active chat runs before downgrading the application or this schema.
DROP TABLE chat_task_supplement;
DELETE FROM task_supplement_capability WHERE issue_id IS NULL;
ALTER TABLE task_supplement_capability ALTER COLUMN issue_id SET NOT NULL;
