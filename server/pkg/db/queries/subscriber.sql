-- name: AddIssueSubscriber :exec
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
VALUES (?, ?, ?, ?)
ON CONFLICT (issue_id, user_type, user_id) DO NOTHING;

-- name: RemoveIssueSubscriber :exec
DELETE FROM issue_subscriber
WHERE issue_id = ? AND user_type = ? AND user_id = ?;

-- name: ListIssueSubscribers :many
SELECT * FROM issue_subscriber
WHERE issue_id = ?
ORDER BY created_at;

-- name: IsIssueSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM issue_subscriber
    WHERE issue_id = ? AND user_type = ? AND user_id = ?
) AS subscribed;
