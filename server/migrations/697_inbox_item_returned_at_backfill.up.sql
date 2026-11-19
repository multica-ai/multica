-- Gives snoozes set before returned_at existed (migration 695) their return
-- rank: each snoozed issue's newest active row ranks by when the snooze ends,
-- as SnoozeInboxGroup now writes it. Only rows still without one are touched,
-- so running it again changes nothing.
UPDATE inbox_item AS item
SET returned_at = item.snoozed_until
FROM (
    SELECT DISTINCT ON (s.workspace_id, s.recipient_type, s.recipient_id, s.issue_id) s.id
    FROM inbox_item s
    WHERE s.archived = false AND s.issue_id IS NOT NULL AND s.snoozed_until IS NOT NULL
    ORDER BY s.workspace_id, s.recipient_type, s.recipient_id, s.issue_id, s.created_at DESC, s.id DESC
) newest
WHERE item.id = newest.id AND item.returned_at IS NULL;
