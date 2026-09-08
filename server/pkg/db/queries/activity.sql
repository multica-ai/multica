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

-- name: ListStatusChangesForIssue :many
-- Every status transition for an issue, oldest first, for the "time in status"
-- aggregate.
--
-- This deliberately does NOT reuse ListActivitiesForIssue. That query caps at
-- the NEWEST N rows, and on a machine-paced issue the cap bites (MUL-5492) —
-- the rows it discards are the oldest ones, which are exactly the ones a
-- duration aggregate needs to attribute the early part of the lifetime. A
-- truncated timeline renders as "some history is missing"; a truncated
-- aggregate renders as a confidently wrong number.
--
-- Uncapped is safe here because the filter is one action out of ~10 logged
-- kinds: status transitions are human/agent-paced (tens per issue), not
-- autosave-paced. idx_activity_log_issue_keyset (migration 068) serves the
-- issue_id lookup via a backward scan; `action` is filtered on the heap rows
-- that scan already touches, so no new index is needed.
-- The ::text casts exist only so sqlc infers `string` instead of `interface{}`
-- for the JSON extractions. COALESCE keeps a missing key from arriving as a
-- NULL that every call site would have to unwrap: "" already means "unknown"
-- in the aggregation below.
SELECT COALESCE(details->>'from', '')::text AS from_status,
       COALESCE(details->>'to', '')::text   AS to_status,
       created_at
FROM activity_log
WHERE issue_id = $1
  AND action = 'status_changed'
ORDER BY created_at ASC, id ASC;

-- name: GetActivity :one
SELECT * FROM activity_log
WHERE id = $1;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, issue_id, actor_type, actor_id, action, details, id
) VALUES ($1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()))
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
