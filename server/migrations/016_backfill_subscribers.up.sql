-- Backfill creators as subscribers
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
SELECT id, creator_type, creator_id, 'creator'
FROM issue
ON CONFLICT DO NOTHING;

-- Backfill assignees as subscribers
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
SELECT id, assignee_type, assignee_id, 'assignee'
FROM issue
WHERE assignee_type IS NOT NULL AND assignee_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Backfill commenters as subscribers
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
SELECT DISTINCT c.issue_id, c.author_type, c.author_id, 'commenter'
FROM comment c
ON CONFLICT DO NOTHING;
