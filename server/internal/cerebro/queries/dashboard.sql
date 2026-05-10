-- name: DashboardCountIssuesByStatus :many
SELECT i.status, COUNT(*)::int AS count
FROM issue i
WHERE i.workspace_id = $1 AND i.kind = 'issue'
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (i.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR i.creator_id = sqlc.narg('actor_id')::uuid))
    OR (i.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('actor_id')::uuid))
    OR EXISTS (
      SELECT 1 FROM activity_log al
      WHERE al.issue_id = i.id
        AND al.actor_type = sqlc.narg('actor_type')::text
        AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid)
    )
  )
GROUP BY i.status;

-- name: DashboardCountIssuesByPriority :many
SELECT i.priority, COUNT(*)::int AS count
FROM issue i
WHERE i.workspace_id = $1 AND i.kind = 'issue'
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (i.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR i.creator_id = sqlc.narg('actor_id')::uuid))
    OR (i.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('actor_id')::uuid))
    OR EXISTS (
      SELECT 1 FROM activity_log al
      WHERE al.issue_id = i.id
        AND al.actor_type = sqlc.narg('actor_type')::text
        AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid)
    )
  )
GROUP BY i.priority;

-- name: DashboardCountIssuesCreatedInPeriod :one
SELECT COUNT(*)::int
FROM issue
WHERE issue.workspace_id = $1 AND issue.kind = 'issue'
  AND created_at >= $2 AND created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (issue.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.creator_id = sqlc.narg('actor_id')::uuid))
    OR (issue.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.assignee_id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardCountIssuesCompletedInPeriod :one
SELECT COUNT(*)::int
FROM issue
WHERE issue.workspace_id = $1 AND issue.kind = 'issue'
  AND issue.status = 'done'
  AND updated_at >= $2 AND updated_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (issue.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.creator_id = sqlc.narg('actor_id')::uuid))
    OR (issue.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.assignee_id = sqlc.narg('actor_id')::uuid))
    OR EXISTS (
      SELECT 1 FROM activity_log al
      WHERE al.issue_id = issue.id
        AND al.actor_type = sqlc.narg('actor_type')::text
        AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid)
        AND al.created_at >= $2 AND al.created_at < $3
    )
  );

-- name: DashboardCountIssuesCreatedByDay :many
SELECT DATE(created_at)::text AS day, COUNT(*)::int AS count
FROM issue
WHERE issue.workspace_id = $1 AND issue.kind = 'issue'
  AND created_at >= $2 AND created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (issue.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.creator_id = sqlc.narg('actor_id')::uuid))
    OR (issue.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.assignee_id = sqlc.narg('actor_id')::uuid))
  )
GROUP BY DATE(created_at)
ORDER BY DATE(created_at);

-- name: DashboardCountIssuesCompletedByDay :many
SELECT DATE(updated_at)::text AS day, COUNT(*)::int AS count
FROM issue
WHERE issue.workspace_id = $1 AND issue.kind = 'issue' AND issue.status = 'done'
  AND updated_at >= $2 AND updated_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (issue.creator_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.creator_id = sqlc.narg('actor_id')::uuid))
    OR (issue.assignee_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR issue.assignee_id = sqlc.narg('actor_id')::uuid))
    OR EXISTS (
      SELECT 1 FROM activity_log al
      WHERE al.issue_id = issue.id
        AND al.actor_type = sqlc.narg('actor_type')::text
        AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid)
        AND al.created_at >= $2 AND al.created_at < $3
    )
  )
GROUP BY DATE(updated_at)
ORDER BY DATE(updated_at);

-- name: DashboardCountChatMessagesInPeriod :one
SELECT COUNT(*)::int
FROM chat_message cm
JOIN chat_session cs ON cs.id = cm.chat_session_id
WHERE cs.workspace_id = $1
  AND cm.created_at >= $2 AND cm.created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'member' AND cm.role = 'user' AND (sqlc.narg('actor_id')::uuid IS NULL OR cs.creator_id = sqlc.narg('actor_id')::uuid))
    OR (sqlc.narg('actor_type')::text = 'agent' AND cm.role = 'assistant' AND (sqlc.narg('actor_id')::uuid IS NULL OR cs.agent_id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardCountChannelMessagesInPeriod :one
-- "Channel messages" = comments on issues with kind in (channel, dm).
SELECT COUNT(*)::int
FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE i.workspace_id = $1 AND i.kind IN ('channel', 'dm')
  AND c.created_at >= $2 AND c.created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (c.author_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR c.author_id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardCountActiveChannelsInPeriod :one
-- Distinct channels/dms with at least one comment in the period.
SELECT COUNT(DISTINCT i.id)::int
FROM issue i
JOIN comment c ON c.issue_id = i.id
WHERE i.workspace_id = $1 AND i.kind IN ('channel', 'dm')
  AND c.created_at >= $2 AND c.created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (c.author_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR c.author_id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardCountTasksByStatusInPeriod :many
-- Buckets tasks by terminal status (completed/failed/cancelled). Tasks still
-- queued or running don't have completed_at set so they're naturally excluded.
SELECT atq.status, COUNT(*)::int AS count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= $2 AND atq.completed_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'agent' AND (sqlc.narg('actor_id')::uuid IS NULL OR a.id = sqlc.narg('actor_id')::uuid))
  )
GROUP BY atq.status;

-- name: DashboardCountTasksByDay :many
SELECT DATE(atq.completed_at)::text AS day, atq.status, COUNT(*)::int AS count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= $2 AND atq.completed_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'agent' AND (sqlc.narg('actor_id')::uuid IS NULL OR a.id = sqlc.narg('actor_id')::uuid))
  )
GROUP BY DATE(atq.completed_at), atq.status
ORDER BY DATE(atq.completed_at);

-- name: DashboardActiveAgentCountInPeriod :one
SELECT COUNT(DISTINCT atq.agent_id)::int
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= $2 AND atq.completed_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'agent' AND (sqlc.narg('actor_id')::uuid IS NULL OR a.id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardActiveMemberCountInPeriod :one
SELECT COUNT(DISTINCT actor_id)::int
FROM activity_log
WHERE workspace_id = $1 AND actor_type = 'member'
  AND created_at >= $2 AND created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'member' AND (sqlc.narg('actor_id')::uuid IS NULL OR actor_id = sqlc.narg('actor_id')::uuid))
  );

-- name: DashboardTopAgentsByActivityInPeriod :many
SELECT a.id::uuid AS actor_id, a.name, COUNT(*)::int AS activity_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= $2 AND atq.completed_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'agent' AND (sqlc.narg('actor_id')::uuid IS NULL OR a.id = sqlc.narg('actor_id')::uuid))
  )
GROUP BY a.id, a.name
ORDER BY COUNT(*) DESC
LIMIT 5;

-- name: DashboardTopMembersByActivityInPeriod :many
SELECT al.actor_id::uuid AS actor_id, u.name AS name, COUNT(*)::int AS activity_count
FROM activity_log al
LEFT JOIN "user" u ON u.id = al.actor_id
WHERE al.workspace_id = $1 AND al.actor_type = 'member'
  AND al.created_at >= $2 AND al.created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'member' AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid))
  )
GROUP BY al.actor_id, u.name
ORDER BY COUNT(*) DESC
LIMIT 5;

-- name: DashboardActivityFeed :many
SELECT al.id::uuid AS id, al.workspace_id, al.issue_id,
       al.actor_type, al.actor_id, al.action, al.details, al.created_at,
       COALESCE(a.name, u.name, initcap(COALESCE(al.actor_type, 'system'))) AS actor_name,
       i.title AS issue_title,
       i.number AS issue_number,
       i.kind AS issue_kind
FROM activity_log al
LEFT JOIN issue i ON i.id = al.issue_id
LEFT JOIN agent a ON a.id = al.actor_id AND al.actor_type = 'agent'
LEFT JOIN "user" u ON u.id = al.actor_id AND al.actor_type = 'member'
WHERE al.workspace_id = $1
  AND al.created_at >= $2 AND al.created_at < $3
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (al.actor_type = sqlc.narg('actor_type')::text AND (sqlc.narg('actor_id')::uuid IS NULL OR al.actor_id = sqlc.narg('actor_id')::uuid))
  )
ORDER BY al.created_at DESC
LIMIT $4;

-- name: DashboardRecentTasks :many
SELECT atq.id::uuid AS task_id, atq.agent_id, atq.issue_id, atq.status,
       atq.dispatched_at, atq.started_at, atq.completed_at, atq.created_at,
       atq.chat_session_id, atq.title AS task_title,
       a.name AS agent_name, a.avatar_url AS agent_avatar_url,
       i.title AS issue_title, i.number AS issue_number
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND (
    sqlc.narg('actor_type')::text IS NULL
    OR (sqlc.narg('actor_type')::text = 'agent' AND (sqlc.narg('actor_id')::uuid IS NULL OR a.id = sqlc.narg('actor_id')::uuid))
  )
ORDER BY COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) DESC
LIMIT $2;
