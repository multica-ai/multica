-- MUL-9788: replying, changing status and reassigning now archive your own
-- notifications on an issue by default. Members who have archived notifications
-- in a workspace keep working the way they do today: those three stay off and
-- the auto_archive_prompt marker asks them once which behavior they want. Their
-- existing auto_archive_completed setting is left alone, and a value they
-- already stored wins over the ones written here.
INSERT INTO notification_preference (workspace_id, user_id, preferences)
SELECT m.workspace_id, m.user_id,
       '{"auto_archive_on_comment": "muted", "auto_archive_on_status": "muted", "auto_archive_on_assignee": "muted", "auto_archive_prompt": "pending"}'::jsonb
FROM member m
WHERE EXISTS (
    SELECT 1 FROM inbox_item i
    WHERE i.workspace_id = m.workspace_id
      AND i.recipient_type = 'member'
      AND i.recipient_id = m.user_id
      AND i.archived
)
ON CONFLICT (workspace_id, user_id) DO UPDATE
SET preferences = EXCLUDED.preferences || notification_preference.preferences,
    updated_at = now();
