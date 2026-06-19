-- CEREBRO-PATCH(cerebro-reminder): FIR-394 — reverse of the reminder entity.
-- Recreate the inbox_item reminder rows from the migrated, still-pending,
-- message-anchored reminders so a rollback restores the prior behaviour, then
-- drop the entity table.
INSERT INTO inbox_item
    (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, body,
     actor_type, actor_id, details, route, muted_until, created_at)
SELECT
    r.workspace_id,
    'member',
    r.user_id,
    'reminder',
    'action_required',
    r.conversation_id,
    'Reminder',
    r.text,
    'member',
    r.user_id,
    jsonb_build_object(
        'due_pending', 'true',
        'planned_at', to_char(r.remind_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
        'text', r.text,
        'comment_id', r.message_id::text
    ),
    'inbox',
    r.remind_at,
    r.created_at
FROM cerebro_reminder r
WHERE r.status = 'pending'
  AND r.message_id IS NOT NULL;

DROP TABLE IF EXISTS cerebro_reminder;
