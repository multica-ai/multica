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
-- One page of the caller's unread issues in one workspace, newest first. Pages
-- continue strictly after the (created_at, issue_id) of the previous page's
-- last row, so every row is reachable however many there are.
-- comment_id is the last live comment the unread run posted on this issue: where
-- opening the issue from the sidebar lands. The run just ended, so its comment
-- is among the issue's newest and the newest-first timeline index finds it fast.
SELECT u.issue_id, u.outcome, u.agent_id, u.task_id, u.created_at, run_comment.id AS comment_id
FROM issue_unread u
LEFT JOIN LATERAL (
  SELECT c.id
  FROM comment c
  WHERE c.issue_id = u.issue_id
    AND c.source_task_id = u.task_id
    AND c.deleted_at IS NULL
  ORDER BY c.created_at DESC, c.id DESC
  LIMIT 1
) run_comment ON true
WHERE u.workspace_id = sqlc.arg(workspace_id)
  AND u.user_id = sqlc.arg(user_id)
  AND (
    sqlc.narg(before_created_at)::timestamptz IS NULL
    OR (u.created_at, u.issue_id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_issue_id)::uuid)
  )
ORDER BY u.created_at DESC, u.issue_id DESC
LIMIT sqlc.arg(page_limit);

-- name: MarkIssueRead :execrows
DELETE FROM issue_unread
WHERE workspace_id = $1 AND user_id = $2 AND issue_id = $3;

-- name: MarkIssueUnreadManually :execrows
-- Keeps an existing row: an unread run result already says more than "manual".
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome)
VALUES ($1, $2, $3, 'manual')
ON CONFLICT (workspace_id, user_id, issue_id) DO NOTHING;
