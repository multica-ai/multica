-- name: MarkIssueUnreadForSubscribers :many
-- An agent run on the issue ended: mark it unread for every active member
-- subscriber who is still in the workspace. Delegated subscriptions (sub-issues
-- an agent filed under the member's issue) are the reduced notification tier
-- and do not light the sidebar. A newer run replaces the row's run details.
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome, agent_id, task_id, created_at)
SELECT i.workspace_id, s.user_id, i.id, sqlc.arg(outcome)::text, sqlc.narg(agent_id)::uuid, sqlc.narg(task_id)::uuid, now()
FROM issue i
JOIN issue_subscriber s
  ON s.issue_id = i.id
 AND s.user_type = 'member'
 AND s.unsubscribed_at IS NULL
 AND s.reason <> 'delegated'
JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = s.user_id
WHERE i.id = sqlc.arg(issue_id)
ON CONFLICT (workspace_id, user_id, issue_id) DO UPDATE
SET outcome = EXCLUDED.outcome,
    agent_id = EXCLUDED.agent_id,
    task_id = EXCLUDED.task_id,
    created_at = EXCLUDED.created_at
RETURNING workspace_id, user_id;

-- name: ListIssueUnread :many
-- The caller's unread issues in one workspace, newest first.
SELECT issue_id, outcome, agent_id, created_at
FROM issue_unread
WHERE workspace_id = $1 AND user_id = $2
ORDER BY created_at DESC
LIMIT 500;

-- name: MarkIssueRead :execrows
DELETE FROM issue_unread
WHERE workspace_id = $1 AND user_id = $2 AND issue_id = $3;

-- name: MarkIssueUnreadManually :execrows
-- Keeps an existing row: an unread run result already says more than "manual".
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome)
VALUES ($1, $2, $3, 'manual')
ON CONFLICT (workspace_id, user_id, issue_id) DO NOTHING;
