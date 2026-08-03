-- name: CreateChildDoneTransition :exec
INSERT INTO child_done_transition (
    group_id, child_issue_id, parent_issue_id, workspace_id,
    terminal_status, stage, transition_at, group_ready, available_at
) VALUES (
    @group_id, @child_issue_id, @parent_issue_id, @workspace_id,
    @terminal_status, @stage, @transition_at, NOT @defer_group::boolean,
    CASE
        WHEN @defer_group::boolean THEN now() + interval '5 minutes'
        ELSE now()
    END
)
ON CONFLICT (child_issue_id, transition_at, terminal_status) DO NOTHING;

-- name: ReleaseChildDoneTransitionGroup :exec
UPDATE child_done_transition
SET group_ready = TRUE,
    available_at = now()
WHERE group_id = @group_id
  AND status = 'queued'
  AND lease_token IS NULL;

-- name: ClaimChildDoneTransitionGroup :many
WITH candidate AS (
    SELECT grouped.group_id
    FROM child_done_transition grouped
    WHERE grouped.group_id = @group_id
      AND grouped.status = 'queued'
    GROUP BY grouped.group_id
    HAVING bool_and(grouped.lease_expires_at IS NULL OR grouped.lease_expires_at <= now())
       AND (bool_and(grouped.group_ready) OR max(grouped.available_at) <= now())
),
lease AS (
    SELECT gen_random_uuid() AS token
)
UPDATE child_done_transition t
SET group_ready = TRUE,
    lease_token = lease.token,
    lease_expires_at = now() + interval '1 minute'
FROM candidate, lease
WHERE t.group_id = candidate.group_id
  AND t.status = 'queued'
  AND (t.lease_expires_at IS NULL OR t.lease_expires_at <= now())
RETURNING t.*;

-- name: ClaimNextChildDoneTransitionGroup :many
WITH candidate AS (
    SELECT grouped.group_id
    FROM (
        SELECT group_id, min(transition_at) AS first_transition_at
        FROM child_done_transition
        WHERE status = 'queued'
        GROUP BY group_id
        HAVING bool_and(lease_expires_at IS NULL OR lease_expires_at <= now())
           AND (bool_and(group_ready) OR max(available_at) <= now())
    ) grouped
    WHERE pg_try_advisory_xact_lock(hashtextextended(grouped.group_id::text, 0))
    ORDER BY grouped.first_transition_at, grouped.group_id
    LIMIT 1
),
lease AS (
    SELECT gen_random_uuid() AS token
)
UPDATE child_done_transition t
SET group_ready = TRUE,
    lease_token = lease.token,
    lease_expires_at = now() + interval '1 minute'
FROM candidate, lease
WHERE t.group_id = candidate.group_id
  AND t.status = 'queued'
  AND (t.lease_expires_at IS NULL OR t.lease_expires_at <= now())
RETURNING t.*;

-- name: CompleteClaimedChildDoneTransitionGroup :many
UPDATE child_done_transition
SET status = 'processed',
    lease_token = NULL,
    lease_expires_at = NULL,
    error = NULL
WHERE group_id = @group_id
  AND status = 'queued'
  AND lease_token = @lease_token
RETURNING *;

-- name: RetryClaimedChildDoneTransitionGroup :many
UPDATE child_done_transition
SET available_at = @available_at,
    lease_token = NULL,
    lease_expires_at = NULL,
    attempts = attempts + 1,
    error = @error
WHERE group_id = @group_id
  AND status = 'queued'
  AND lease_token = @lease_token
RETURNING *;

-- name: GetChildDoneTransitionGroup :many
SELECT *
FROM child_done_transition
WHERE group_id = @group_id
ORDER BY transition_at, child_issue_id;

-- name: GetChildDoneBarrierGeneration :one
-- The latest matching terminal transition is stable across independent worker
-- groups that observe the same closed barrier. It changes only when a child
-- enters a new terminal generation, such as after being reopened.
SELECT max(t.transition_at)::timestamptz
FROM child_done_transition t
JOIN issue i ON i.id = t.child_issue_id
WHERE t.parent_issue_id = @parent_issue_id
  AND i.parent_issue_id = @parent_issue_id
  AND i.status = t.terminal_status
  AND (NOT @staged::boolean OR i.stage = @stage);

-- name: DeleteChildDoneTransitionsByIssue :exec
DELETE FROM child_done_transition
WHERE child_issue_id = @issue_id OR parent_issue_id = @issue_id;
