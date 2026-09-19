-- name: CreateAgentTaskCompletionOutcome :one
INSERT INTO agent_task_completion_outcome (
    task_id, workspace_id, issue_id, agent_id, outcome_kind, content,
    result_sha256, final_comment_id, answered_comment_ids
) VALUES (
    @task_id, @workspace_id, @issue_id, @agent_id, @outcome_kind, @content,
    @result_sha256, sqlc.narg(final_comment_id), @answered_comment_ids
)
RETURNING *;

-- name: GetAgentTaskCompletionOutcome :one
SELECT * FROM agent_task_completion_outcome WHERE task_id = @task_id;

-- name: CreateTaskCompletionOutbox :one
INSERT INTO task_completion_outbox (
    task_id, workspace_id, issue_id, agent_id, comment_id, issue_revision, state,
    published_at
) VALUES (
    @task_id, @workspace_id, @issue_id, @agent_id, @comment_id, @issue_revision,
    @state, CASE WHEN @state::text = 'published' THEN now() ELSE NULL END
)
RETURNING *;

-- name: ClaimTaskCompletionOutbox :many
WITH candidates AS (
    SELECT candidate.task_id
    FROM task_completion_outbox AS candidate
    WHERE (
            candidate.state = 'pending'
            OR (candidate.state = 'publishing' AND candidate.lease_expires_at <= now())
          )
      AND (candidate.next_attempt_at IS NULL OR candidate.next_attempt_at <= now())
      AND candidate.attempts < @max_attempts
    ORDER BY COALESCE(candidate.next_attempt_at, candidate.created_at), candidate.created_at, candidate.task_id
    FOR UPDATE SKIP LOCKED
    LIMIT @row_limit
)
UPDATE task_completion_outbox AS outbox
SET state = 'publishing',
    attempts = outbox.attempts + 1,
    lease_owner = @lease_owner,
    lease_expires_at = now() + make_interval(secs => @lease_seconds::double precision),
    updated_at = now()
FROM candidates
WHERE outbox.task_id = candidates.task_id
RETURNING outbox.*;

-- name: MarkTaskCompletionOutboxPublished :execrows
UPDATE task_completion_outbox
SET state = 'published', published_at = now(), lease_owner = NULL,
    lease_expires_at = NULL, next_attempt_at = NULL, last_error = NULL,
    updated_at = now()
WHERE task_id = @task_id AND state = 'publishing' AND lease_owner = @lease_owner;

-- name: RetryTaskCompletionOutbox :execrows
UPDATE task_completion_outbox
SET state = CASE WHEN attempts >= @max_attempts THEN 'dead' ELSE 'pending' END,
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_attempt_at = CASE WHEN attempts >= @max_attempts THEN NULL ELSE @next_attempt_at END,
    last_error = @last_error,
    updated_at = now()
WHERE task_id = @task_id AND state = 'publishing' AND lease_owner = @lease_owner;
