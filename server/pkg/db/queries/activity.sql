-- name: ListActivitiesForIssue :many
-- All activities for an issue in chronological order, capped at $2 (DB safety
-- net to bound the response).
SELECT * FROM activity_log
WHERE issue_id = $1
ORDER BY created_at ASC, id ASC
LIMIT $2;

-- name: GetActivity :one
SELECT * FROM activity_log
WHERE id = $1;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, issue_id, actor_type, actor_id, action, details
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: HasSquadLeaderNoActionEvaluationForTask :one
SELECT EXISTS (
  SELECT 1
  FROM activity_log
  WHERE issue_id = @issue_id
    AND actor_type = 'agent'
    AND actor_id = @agent_id
    AND action = 'squad_leader_evaluated'
    AND details->>'outcome' = 'no_action'
    AND details->>'task_id' = @task_id::text
) AS exists;

-- name: ListSpaceLifecycleActivities :many
-- Space Settings only needs governance events for this one Space, newest
-- first. Actor display data is resolved here so deleted/missing actors safely
-- fall back to the stored actor type in the UI.
SELECT
  al.id,
  al.actor_type,
  al.actor_id,
  al.action,
  al.details,
  al.created_at,
  COALESCE(CASE
    WHEN al.actor_type = 'member' THEN u.name
    WHEN al.actor_type = 'agent' THEN a.name
    ELSE NULL
  END, '')::text AS actor_name,
  COALESCE(CASE
    WHEN al.actor_type = 'member' THEN u.avatar_url
    WHEN al.actor_type = 'agent' THEN a.avatar_url
    ELSE NULL
  END, '')::text AS actor_avatar_url
FROM activity_log al
LEFT JOIN "user" u ON al.actor_type = 'member' AND al.actor_id = u.id
LEFT JOIN agent a ON al.actor_type = 'agent' AND al.actor_id = a.id
WHERE al.workspace_id = @workspace_id
  AND al.action IN (
    'space_archived',
    'space_restored',
    'space_autopilots_resumed',
    'integration_space_bindings_replaced'
  )
  AND (
    al.details->>'space_id' = @space_id::text
    OR (
      al.action = 'integration_space_bindings_replaced'
      AND al.details->'affected_space_ids' ? @space_id::text
    )
  )
ORDER BY al.created_at DESC, al.id DESC
LIMIT 100;

-- name: CountAssigneeChangesByActor :many
-- Count how many times a user assigned each target via assignee_changed activities.
SELECT
  details->>'to_type' as assignee_type,
  details->>'to_id' as assignee_id,
  COUNT(*)::bigint as frequency
FROM activity_log
WHERE workspace_id = $1
  AND actor_id = $2
  AND actor_type = 'member'
  AND action = 'assignee_changed'
  AND details->>'to_type' IS NOT NULL
  AND details->>'to_id' IS NOT NULL
GROUP BY details->>'to_type', details->>'to_id';
