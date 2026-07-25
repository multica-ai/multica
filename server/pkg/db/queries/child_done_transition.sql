-- name: CreateChildDoneTransition :exec
INSERT INTO child_done_transition (
    group_id, child_issue_id, parent_issue_id, workspace_id,
    terminal_status, stage, transition_at, available_at
) VALUES (
    @group_id, @child_issue_id, @parent_issue_id, @workspace_id,
    @terminal_status, @stage, @transition_at,
    CASE
        WHEN @defer_group::boolean THEN now() + interval '5 minutes'
        ELSE now()
    END
)
ON CONFLICT (child_issue_id, transition_at, terminal_status) DO NOTHING;

-- name: ReleaseChildDoneTransitionGroup :exec
UPDATE child_done_transition
SET available_at = now()
WHERE group_id = @group_id
  AND status = 'queued'
  AND lease_token IS NULL;

-- name: ClaimChildDoneTransitionGroup :many
WITH lease AS (
    SELECT gen_random_uuid() AS token
)
UPDATE child_done_transition t
SET lease_token = lease.token,
    lease_expires_at = now() + interval '1 minute'
FROM lease
WHERE t.group_id = @group_id
  AND t.status = 'queued'
  AND t.available_at <= now()
  AND (t.lease_expires_at IS NULL OR t.lease_expires_at <= now())
RETURNING t.*;

-- name: ClaimNextChildDoneTransitionGroup :many
WITH candidate AS (
    SELECT grouped.group_id
    FROM (
        SELECT group_id, min(transition_at) AS first_transition_at
        FROM child_done_transition
        WHERE status = 'queued'
          AND available_at <= now()
          AND (lease_expires_at IS NULL OR lease_expires_at <= now())
        GROUP BY group_id
    ) grouped
    WHERE pg_try_advisory_xact_lock(hashtextextended(grouped.group_id::text, 0))
    ORDER BY grouped.first_transition_at, grouped.group_id
    LIMIT 1
),
lease AS (
    SELECT gen_random_uuid() AS token
)
UPDATE child_done_transition t
SET lease_token = lease.token,
    lease_expires_at = now() + interval '1 minute'
FROM candidate, lease
WHERE t.group_id = candidate.group_id
  AND t.status = 'queued'
  AND t.available_at <= now()
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

-- name: DeleteChildDoneTransitionsByIssue :exec
DELETE FROM child_done_transition
WHERE child_issue_id = @issue_id OR parent_issue_id = @issue_id;
