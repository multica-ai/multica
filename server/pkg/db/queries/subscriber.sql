-- name: AddIssueSubscriber :exec
INSERT INTO multica_issue_subscriber (issue_id, user_type, user_id, reason)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id, user_type, user_id) DO NOTHING;

-- name: RemoveIssueSubscriber :exec
DELETE FROM multica_issue_subscriber
WHERE issue_id = $1 AND user_type = $2 AND user_id = $3;

-- name: ListIssueSubscribers :many
SELECT * FROM multica_issue_subscriber
WHERE issue_id = $1
ORDER BY created_at;

-- name: IsIssueSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM multica_issue_subscriber
    WHERE issue_id = $1 AND user_type = $2 AND user_id = $3
) AS subscribed;
