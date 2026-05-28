-- name: GetAutoSubscribePreference :one
SELECT * FROM auto_subscribe_preference
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertAutoSubscribePreference :one
INSERT INTO auto_subscribe_preference (
  workspace_id,
  user_id,
  issue_creator,
  issue_assignee,
  comment_author,
  issue_description_mention,
  comment_mention,
  quick_create_requester
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET
  issue_creator = EXCLUDED.issue_creator,
  issue_assignee = EXCLUDED.issue_assignee,
  comment_author = EXCLUDED.comment_author,
  issue_description_mention = EXCLUDED.issue_description_mention,
  comment_mention = EXCLUDED.comment_mention,
  quick_create_requester = EXCLUDED.quick_create_requester,
  updated_at = now()
RETURNING *;
