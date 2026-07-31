-- name: ListActivitiesForIssue :many
-- The NEWEST $2 activities for an issue, returned in chronological order.
--
-- The cap has to bite at the OLD end, not the new one. The inner query takes
-- the window with the keyset ordering (created_at DESC, id DESC), which
-- idx_activity_log_issue_keyset (migration 068) satisfies without a sort step —
-- it is not an index-only scan, since the index does not cover the columns
-- SELECT * needs, so the heap is still read for the rows in the window. The
-- outer query re-sorts ascending so every caller keeps the chronological
-- contract it already had.
--
-- Capping with ORDER BY created_at ASC instead discards the newest rows, which
-- made a busy issue's timeline appear to stop at some point in the past with no
-- indication anything was missing. Activity is machine-paced (description
-- autosave, every agent run, status/assignee changes), so this was reachable in
-- normal use, not only on pathological issues (MUL-5492).
SELECT * FROM (
    SELECT * FROM activity_log
    WHERE issue_id = $1
    ORDER BY created_at DESC, id DESC
    LIMIT $2
) AS recent
ORDER BY created_at ASC, id ASC;

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
