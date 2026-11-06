DROP TABLE IF EXISTS chat_supplement;
DELETE FROM task_supplement_capability WHERE issue_id IS NULL;
ALTER TABLE task_supplement_capability ALTER COLUMN issue_id SET NOT NULL;
