-- name: UpsertCommentDeliveryObligation :one
-- Only persist references in the selected workspace. An old admission racing
-- an edit cannot replace a newer revision or reset its retry schedule.
WITH admission_agent AS MATERIALIZED (
    SELECT a.id, a.workspace_id FROM agent a
    WHERE a.id = @agent_id AND a.workspace_id = @workspace_id
    FOR KEY SHARE OF a
), admission_owners AS MATERIALIZED (
    SELECT a.id, a.workspace_id,
           lock_task_owner_rows(a.id, (
               SELECT c.issue_id FROM comment c
               WHERE c.id = @comment_id AND c.workspace_id = a.workspace_id
           ), NULL)::boolean AS locked
    FROM admission_agent a
), admission_source AS MATERIALIZED (
    -- Recheck the locked tuple after an owner/edit wait, before INSERT can
    -- wait on a withdrawn obligation and reuse its older statement snapshot.
    SELECT c.* FROM comment c
    JOIN admission_owners a ON a.workspace_id = c.workspace_id AND a.locked
    JOIN issue i ON i.id = c.issue_id AND i.workspace_id = c.workspace_id
    WHERE c.id = @comment_id
      AND c.revision = @source_revision AND c.content = @source_content
      AND c.deleted_at IS NULL
    FOR SHARE OF c
)
INSERT INTO comment_delivery_obligation (
    workspace_id, issue_id, comment_id, agent_id, comment_revision, comment_content, input_updated_at,
    actor_type, actor_id, originator_user_id, authoring_task_id
)
SELECT c.workspace_id, c.issue_id, c.id, a.id, c.revision, c.content, c.updated_at,
       @actor_type, sqlc.narg(actor_id)::uuid, sqlc.narg(originator_user_id)::uuid, sqlc.narg(authoring_task_id)::uuid
FROM admission_source c
JOIN admission_agent a ON a.workspace_id = c.workspace_id
ON CONFLICT (comment_id, agent_id) DO UPDATE
SET comment_revision = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.comment_revision ELSE comment_delivery_obligation.comment_revision
    END,
    comment_content = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
        THEN EXCLUDED.comment_content ELSE comment_delivery_obligation.comment_content
    END,
    actor_type = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.actor_type ELSE comment_delivery_obligation.actor_type
    END,
    actor_id = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.actor_id ELSE comment_delivery_obligation.actor_id
    END,
    originator_user_id = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.originator_user_id ELSE comment_delivery_obligation.originator_user_id
    END,
    authoring_task_id = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.authoring_task_id ELSE comment_delivery_obligation.authoring_task_id
    END,
    input_updated_at = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content
        THEN EXCLUDED.input_updated_at ELSE comment_delivery_obligation.input_updated_at
    END,
    next_attempt_at = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content THEN now()
        ELSE comment_delivery_obligation.next_attempt_at
    END,
    lease_until = CASE
        WHEN EXCLUDED.comment_revision > comment_delivery_obligation.comment_revision
             AND EXCLUDED.comment_content IS DISTINCT FROM comment_delivery_obligation.comment_content THEN '-infinity'::timestamptz
        ELSE comment_delivery_obligation.lease_until
    END
RETURNING *;

-- name: GetCommentDeliveryObligation :one
SELECT * FROM comment_delivery_obligation
WHERE workspace_id = @workspace_id AND comment_id = @comment_id AND agent_id = @agent_id;

-- name: DeleteCommentDeliveryObligation :exec
-- A drain of an older comment revision must not settle a later edit.
DELETE FROM comment_delivery_obligation
WHERE workspace_id = @workspace_id AND comment_id = @comment_id
  AND agent_id = @agent_id AND comment_revision = @comment_revision;

-- name: ClaimCommentDeliveryObligations :many
-- Retry backoff is independent of active ownership. Completed attempts release
-- their lease; crashed attempts become eligible after the lease expires.
WITH due AS (
    SELECT workspace_id, comment_id, agent_id
    FROM comment_delivery_obligation
    WHERE next_attempt_at <= now() AND lease_until <= now()
    ORDER BY next_attempt_at, comment_id, agent_id
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
)
UPDATE comment_delivery_obligation o
SET next_attempt_at = now() + interval '30 seconds',
    lease_until = now() + interval '30 seconds'
FROM due
WHERE o.workspace_id = due.workspace_id AND o.comment_id = due.comment_id AND o.agent_id = due.agent_id
RETURNING o.*;

-- name: CommentDeliveryObligationCoverage :one
SELECT
    EXISTS (
        SELECT 1 FROM agent_task_queue t
        JOIN comment c ON c.id = @comment_id AND c.workspace_id = @workspace_id
        JOIN comment_delivery_obligation o ON o.comment_id = c.id AND o.workspace_id = c.workspace_id AND o.agent_id = @agent_id
        WHERE t.issue_id = c.issue_id AND t.agent_id = @agent_id
          AND (
              (t.status IN ('completed', 'failed', 'cancelled')
               AND c.id = ANY(t.delivered_comment_ids)
               AND (
                   (jsonb_typeof(t.context -> 'delivered_comment_revisions' -> c.id::text) = 'number'
                    AND t.context -> 'delivered_comment_revisions' -> c.id::text >= to_jsonb(o.comment_revision))
                   -- Older claimers write IDs only. Never let that fallback
                   -- override an explicit revision or acknowledge a later edit.
                   OR (t.context -> 'delivered_comment_revisions' -> c.id::text IS NULL
                       AND t.dispatched_at >= o.input_updated_at)
               ))
              OR (t.status = 'cancelled' AND t.cancelled_by_type IN ('member', 'agent')
                  AND t.cancelled_by_id IS NOT NULL
                  AND jsonb_typeof(t.context -> 'cancelled_comment_revisions' -> c.id::text) = 'number'
                  AND t.context -> 'cancelled_comment_revisions' -> c.id::text >= to_jsonb(o.comment_revision)
                  AND (t.trigger_comment_id = c.id OR c.id = ANY(t.coalesced_comment_ids)))
          )
    ) OR EXISTS (
        -- Stop after two system-terminated tasks created for this input.
        -- Earlier coalesced tasks do not consume a new text version's budget.
        -- Repairing a batch after a comment edit/delete is recoverable; only
        -- a marker for this exact task excludes its cancellation from the limit.
        SELECT 1 FROM agent_task_queue t
        JOIN comment_delivery_obligation o
          ON o.comment_id = @comment_id AND o.workspace_id = @workspace_id AND o.agent_id = @agent_id
        WHERE t.issue_id = o.issue_id AND t.agent_id = o.agent_id
          AND t.created_at >= o.input_updated_at
          AND (t.trigger_comment_id = o.comment_id OR o.comment_id = ANY(t.coalesced_comment_ids))
          AND (t.status = 'failed' OR (
              t.status = 'cancelled' AND (t.cancelled_by_type IS NULL OR t.cancelled_by_type = 'system')
              AND (t.context ->> 'comment_change_cancelled_task_id') IS DISTINCT FROM t.id::text
          ))
        LIMIT 1 OFFSET 1
    ) OR EXISTS (
        SELECT 1 FROM task_supplement s
        JOIN agent_task_queue t ON t.id = s.task_id
        JOIN comment c ON c.id = s.comment_id AND c.workspace_id = s.workspace_id
        WHERE s.workspace_id = @workspace_id AND s.comment_id = @comment_id AND t.agent_id = @agent_id
          AND (c.author_type <> 'agent' OR s.status = 'delivered')
    ) AS settled,
    EXISTS (
        SELECT 1 FROM agent_task_queue t
        JOIN comment c ON c.id = @comment_id AND c.workspace_id = @workspace_id
        WHERE t.issue_id = c.issue_id AND t.agent_id = @agent_id
          AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
          AND (t.trigger_comment_id = c.id OR c.id = ANY(t.coalesced_comment_ids))
    ) AS planned;

-- name: DeleteCommentDeliveryObligationsForIssue :exec
DELETE FROM comment_delivery_obligation o USING issue i
WHERE i.id = @issue_id AND o.issue_id = i.id AND o.workspace_id = i.workspace_id;

-- name: DeleteCommentDeliveryObligationsForAgent :exec
DELETE FROM comment_delivery_obligation o USING agent a
WHERE a.id = @agent_id AND o.agent_id = a.id AND o.workspace_id = a.workspace_id;

-- name: DeleteCommentDeliveryObligationsForComment :exec
DELETE FROM comment_delivery_obligation o USING comment c
WHERE c.id = @comment_id AND o.comment_id = c.id AND o.workspace_id = c.workspace_id;

-- name: TryClaimCommentDeliveryObligation :one
-- Producers and background drainers share the same claim boundary. Re-admission
-- cannot dispatch a second run while another caller owns this revision.
UPDATE comment_delivery_obligation
SET lease_until = now() + interval '30 seconds',
    next_attempt_at = now() + interval '30 seconds'
WHERE workspace_id = @workspace_id AND comment_id = @comment_id AND agent_id = @agent_id
  AND comment_revision = @comment_revision AND lease_until <= now()
RETURNING *;

-- name: ReleaseCommentDeliveryObligation :exec
UPDATE comment_delivery_obligation
SET lease_until = '-infinity'
WHERE workspace_id = @workspace_id AND comment_id = @comment_id AND agent_id = @agent_id
  AND comment_revision = @comment_revision AND lease_until = @lease_until;

-- name: LockAgentForCommentDelivery :one
-- Agent first, before the owner fence and accepted input locks.
SELECT * FROM agent
WHERE id = @agent_id AND workspace_id = @workspace_id AND kind = 'user'
FOR KEY SHARE;

-- name: LockCommentDeliveryOwners :one
SELECT lock_task_owner_rows(@agent_id::uuid, @issue_id::uuid, @runtime_id::uuid)::boolean AS locked;

-- name: LockCommentDeliverySource :one
SELECT * FROM comment
WHERE id = @comment_id AND workspace_id = @workspace_id
FOR SHARE;

-- name: LockClaimedCommentDeliveryObligation :one
SELECT * FROM comment_delivery_obligation
WHERE workspace_id = @workspace_id AND comment_id = @comment_id AND agent_id = @agent_id
  AND comment_revision = @comment_revision AND lease_until = @lease_until
  AND lease_until > clock_timestamp()
FOR UPDATE;
