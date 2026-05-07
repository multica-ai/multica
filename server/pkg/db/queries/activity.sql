-- name: ListActivitiesLatest :many
-- Top N activities for an issue, newest first. Used by the cursor-paginated
-- timeline endpoint to assemble the latest page.
SELECT * FROM activity_log
WHERE issue_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ListActivitiesBefore :many
SELECT * FROM activity_log
WHERE issue_id = $1
  AND (created_at, id) < ($2::timestamptz, $3::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $4;

-- name: ListActivitiesAfter :many
SELECT * FROM activity_log
WHERE issue_id = $1
  AND (created_at, id) > ($2::timestamptz, $3::uuid)
ORDER BY created_at ASC, id ASC
LIMIT $4;

-- name: GetActivity :one
-- Used by the around-id mode of ListTimeline to resolve an entry to its
-- (created_at, id) cursor when the entry is an activity.
SELECT * FROM activity_log
WHERE id = $1;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, issue_id, actor_type, actor_id, action, details
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

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
