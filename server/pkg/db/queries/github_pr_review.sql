-- name: ListLinkedIssuesForGitHubPR :many
-- Resolves every linked Multica issue across workspaces bound to a GitHub App
-- installation. Unlinked PRs return zero rows immediately.
-- LinkIssueToPullRequestManually stamps linked_by_type = 'member' even when an
-- agent invokes it via task token, so callers resolve linked_by_id against agent.
SELECT
    sqlc.embed(w),
    sqlc.embed(i),
    pr.id AS pull_request_id,
    pr.state AS pr_state,
    pr.html_url AS pr_html_url,
    pr.head_sha AS pr_head_sha,
    COALESCE(pr.author_login, '')::text AS pr_author_login,
    ipr.linked_by_id
FROM github_installation gi
JOIN workspace w ON w.id = gi.workspace_id
JOIN github_pull_request pr
  ON pr.workspace_id = gi.workspace_id
 AND pr.repo_owner = @repo_owner
 AND pr.repo_name = @repo_name
 AND pr.pr_number = @pr_number
JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
JOIN issue i ON i.id = ipr.issue_id AND i.workspace_id = pr.workspace_id
WHERE gi.installation_id = @installation_id
ORDER BY gi.created_at ASC, gi.id ASC, i.created_at ASC, i.id ASC;

-- name: LockGitHubPRReviewSync :one
-- Serializes PR review sync + round accounting for one (issue, pull_request)
-- pair without taking table-row locks out of lock_task_owner_rows order.
SELECT now()::timestamptz AS locked_at
FROM (
    SELECT pg_advisory_xact_lock(
        hashtextextended(sqlc.arg('issue_id')::uuid::text || ':' || sqlc.arg('pull_request_id')::uuid::text || ':gh_pr_review', 0)
    )
) AS sync_lock;

-- name: ExistsGitHubPRReviewDelivery :one
SELECT EXISTS (
    SELECT 1
    FROM github_pr_review_delivery
    WHERE issue_id = @issue_id
      AND event_type = @event_type
      AND github_event_id = @github_event_id
);

-- name: CreateGitHubPRReviewDelivery :one
INSERT INTO github_pr_review_delivery (
    id, workspace_id, issue_id, pull_request_id, event_type, github_event_id, comment_id
) VALUES (
    @id, @workspace_id, @issue_id, @pull_request_id, @event_type, @github_event_id, @comment_id
)
ON CONFLICT (issue_id, event_type, github_event_id) DO NOTHING
RETURNING *;

-- name: SetGitHubPRReviewDeliveryTask :exec
UPDATE github_pr_review_delivery
SET triggered_task_id = @triggered_task_id
WHERE id = @id;

-- name: GetGitHubPRReviewWakeupState :one
SELECT *
FROM github_pr_review_wakeup_state
WHERE issue_id = @issue_id
  AND pull_request_id = @pull_request_id;

-- name: UpsertGitHubPRReviewWakeupState :one
INSERT INTO github_pr_review_wakeup_state (
    issue_id, pull_request_id, workspace_id, consecutive_rounds,
    active_round_task_id, active_round_review_id, last_triggered_at, updated_at
) VALUES (
    @issue_id, @pull_request_id, @workspace_id, @consecutive_rounds,
    sqlc.narg('active_round_task_id'), sqlc.narg('active_round_review_id'), now(), now()
)
ON CONFLICT (issue_id, pull_request_id) DO UPDATE SET
    consecutive_rounds = EXCLUDED.consecutive_rounds,
    active_round_task_id = EXCLUDED.active_round_task_id,
    active_round_review_id = EXCLUDED.active_round_review_id,
    last_triggered_at = EXCLUDED.last_triggered_at,
    updated_at = now()
RETURNING *;

-- name: HasHumanIssueInterventionSince :one
-- Detects whether a workspace member intervened on the Multica issue since the
-- last automated PR review round (member comment, manual human run/rerun, or
-- member issue update), which resets the 2-round consecutive wakeup cap.
SELECT (
    EXISTS (
        SELECT 1
        FROM comment c
        WHERE c.issue_id = @issue_id
          AND c.workspace_id = @workspace_id
          AND c.author_type = 'member'
          AND c.deleted_at IS NULL
          AND c.created_at > @since
    )
    OR EXISTS (
        SELECT 1
        FROM agent_task_queue t
        WHERE t.issue_id = @issue_id
          AND t.originator_source = 'direct_human'
          AND t.created_at > @since
          AND (
              t.rerun_of_task_id IS NOT NULL
              OR NOT EXISTS (
                  SELECT 1
                  FROM github_pr_review_delivery d
                  WHERE d.issue_id = t.issue_id
                    AND (d.triggered_task_id = t.id OR d.comment_id = t.trigger_comment_id)
              )
          )
    )
    OR EXISTS (
        SELECT 1
        FROM activity_log al
        WHERE al.issue_id = @issue_id
          AND al.workspace_id = @workspace_id
          AND al.actor_type = 'member'
          AND al.created_at > @since
    )
)::boolean AS intervened;

-- name: HasQueuedTaskForIssueAndAgent :one
-- Uses the exact same target predicate as CoalesceGitHubPRReviewCommentIntoQueuedTask
-- so a round-2 check only stays open when coalescing can actually absorb the comment.
SELECT EXISTS (
    SELECT 1
    FROM agent_task_queue t
    WHERE t.context->>'wakeup_id' IS NULL
      AND t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND (
          sqlc.narg('new_leader_squad_id')::uuid IS NULL
          OR (t.is_leader_task AND t.squad_id = sqlc.narg('new_leader_squad_id')::uuid)
          OR (NOT t.is_leader_task AND t.squad_id IS NULL)
      )
      AND (
          t.status = 'queued'
          OR (t.status = 'deferred' AND t.context->>'channel_issue_media_pending' = 'true')
      )
      AND (
          COALESCE(sqlc.narg('head_sha')::text, '') = ''
          OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
      )
);

-- name: CoalesceGitHubPRReviewCommentIntoQueuedTask :one
-- Folds a system-authored GitHub PR review comment into an unclaimed queued
-- task for (issue, agent) while preserving the queued run's existing human
-- attribution snapshot and MCP overlay.
UPDATE agent_task_queue
SET coalesced_comment_ids = (
        SELECT COALESCE(array_agg(DISTINCT e), '{}')
        FROM unnest(array_append(coalesced_comment_ids, trigger_comment_id)) AS e
        WHERE e IS NOT NULL AND e <> @new_trigger_comment_id::uuid
    ),
    is_leader_task = CASE WHEN sqlc.narg('new_leader_squad_id')::uuid IS NOT NULL
        THEN TRUE ELSE is_leader_task END,
    squad_id = COALESCE(sqlc.narg('new_leader_squad_id')::uuid, squad_id),
    trigger_comment_id = @new_trigger_comment_id::uuid,
    comment_thread_id = comment_thread_root_id(@new_trigger_comment_id::uuid),
    trigger_summary = COALESCE(sqlc.narg('new_trigger_summary'), trigger_summary)
WHERE id = (
    SELECT t.id FROM agent_task_queue t
    WHERE t.context->>'wakeup_id' IS NULL
      AND t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND (
          sqlc.narg('new_leader_squad_id')::uuid IS NULL
          OR (t.is_leader_task AND t.squad_id = sqlc.narg('new_leader_squad_id')::uuid)
          OR (NOT t.is_leader_task AND t.squad_id IS NULL)
      )
      AND (
          t.status = 'queued'
          OR (t.status = 'deferred' AND t.context->>'channel_issue_media_pending' = 'true')
      )
      AND (
          COALESCE(sqlc.narg('head_sha')::text, '') = ''
          OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
      )
    ORDER BY t.created_at DESC
    LIMIT 1
    FOR UPDATE OF t
)
RETURNING *;

-- name: ListRunnableIssueDeveloperTasks :many
-- Lists recent non-cancelled tasks on an issue executed by active, runtime-bound
-- user agents so PR review routing can wake the developer agent or squad leader
-- even after the issue moved to in_review / shipping.
SELECT
    t.id,
    t.agent_id,
    t.squad_id,
    t.usage_squad_id,
    t.is_leader_task,
    t.originator_user_id,
    COALESCE(t.workflow_step, '')::text AS workflow_step,
    t.created_at
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.issue_id = @issue_id
  AND t.status <> 'cancelled'
  AND a.workspace_id = @workspace_id
  AND a.archived_at IS NULL
  AND a.runtime_id IS NOT NULL
  AND a.kind = 'user'
ORDER BY t.created_at DESC, t.id DESC
LIMIT 20;
