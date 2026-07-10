-- name: AddIssueSubscriber :exec
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id, user_type, user_id) DO NOTHING;

-- name: RemoveIssueSubscriber :exec
DELETE FROM issue_subscriber
WHERE issue_id = $1 AND user_type = $2 AND user_id = $3;

-- name: ListIssueSubscribers :many
SELECT * FROM issue_subscriber
WHERE issue_id = $1
ORDER BY created_at;

-- name: ListIssueNotificationRecipients :many
-- Space followers are a dynamic notification audience. Following grants no
-- access: Private Space followers remain eligible only while they still have
-- Space membership (Workspace owners/admins retain their governance access).
SELECT s.user_type, s.user_id
FROM issue_subscriber s
WHERE s.issue_id = $1
UNION
SELECT 'member'::text AS user_type, pref.user_id
FROM issue i
JOIN workspace_space ws
  ON ws.id = i.space_id AND ws.workspace_id = i.workspace_id
JOIN workspace_space_preference pref
  ON pref.workspace_id = i.workspace_id
 AND pref.space_id = i.space_id
 AND pref.is_followed = true
JOIN member wm
  ON wm.workspace_id = i.workspace_id AND wm.user_id = pref.user_id
LEFT JOIN workspace_space_member sm
  ON sm.space_id = i.space_id AND sm.user_id = pref.user_id
WHERE i.id = $1
  AND (ws.visibility = 'open' OR sm.user_id IS NOT NULL OR wm.role IN ('owner', 'admin'));

-- name: IsIssueSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM issue_subscriber
    WHERE issue_id = $1 AND user_type = $2 AND user_id = $3
) AS subscribed;
