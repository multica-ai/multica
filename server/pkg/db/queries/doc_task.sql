-- Agent runs started from doc discussions (MUL-9933). A run records the doc,
-- the discussion's first comment (doc_thread_id) and the comment that
-- triggered it (doc_comment_id); the doc-scoped task context JSON carries the
-- workspace so events still resolve after the doc is gone.

-- name: CreateDocCommentTask :one
-- Fenced against workspace teardown like every other enqueue: the caller has
-- already locked the agent row; lock_task_owner_rows locks the workspace and
-- runtime rows inside this statement and returns false once they are gone.
WITH enqueue_agent AS MATERIALIZED (
    SELECT a.id, a.runtime_id
    FROM agent a
    WHERE a.id = sqlc.arg(agent_id)
      AND lock_task_owner_rows(a.id, NULL, a.runtime_id)
    FOR KEY SHARE OF a
)
INSERT INTO agent_task_queue (
    id, agent_id, runtime_id, issue_id, status, priority, context, trigger_summary,
    doc_id, doc_thread_id, doc_comment_id,
    originator_user_id, accountable_user_id, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, delegated_from_task_id, trigger_evidence_kind, trigger_evidence_ref_id
)
SELECT
    sqlc.arg(id), sqlc.arg(agent_id), enqueue_agent.runtime_id, NULL, 'queued',
    sqlc.arg(priority), sqlc.arg(context), sqlc.narg(trigger_summary),
    sqlc.arg(doc_id), sqlc.arg(doc_thread_id), sqlc.arg(doc_comment_id),
    sqlc.narg(originator_user_id), sqlc.narg(accountable_user_id),
    sqlc.narg(runtime_mcp_overlay), sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source), sqlc.narg(delegated_from_task_id),
    sqlc.narg(trigger_evidence_kind), sqlc.narg(trigger_evidence_ref_id)
FROM enqueue_agent
RETURNING *;

-- name: GetQueuedDocThreadTaskForUpdate :one
-- The agent's run for this discussion that has not been picked up yet. A new
-- comment folds into it instead of queuing a second run.
SELECT * FROM agent_task_queue
WHERE doc_id = @doc_id AND doc_thread_id = @doc_thread_id AND agent_id = @agent_id
  AND status = 'queued'
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: MergeDocCommentIntoQueuedTask :one
-- The newer comment becomes the run's trigger, and the run moves to that
-- comment's human with its whole attribution and connected apps, as
-- MergeCommentIntoPendingTask does for issues: a run answering B must not run
-- with A's apps or show A as accountable.
UPDATE agent_task_queue
SET doc_comment_id = @doc_comment_id,
    trigger_summary = sqlc.narg(trigger_summary),
    originator_user_id = sqlc.narg(originator_user_id)::uuid,
    accountable_user_id = sqlc.narg(accountable_user_id)::uuid,
    originator_source = sqlc.narg(originator_source),
    delegated_from_task_id = sqlc.narg(delegated_from_task_id)::uuid,
    trigger_evidence_kind = sqlc.narg(trigger_evidence_kind),
    trigger_evidence_ref_id = sqlc.narg(trigger_evidence_ref_id)::uuid,
    runtime_mcp_overlay = sqlc.narg(runtime_mcp_overlay),
    runtime_connected_apps = sqlc.narg(runtime_connected_apps)
WHERE id = @id AND status = 'queued'
RETURNING *;

-- name: GetDocThreadContinuationAgent :one
-- The agent that most recently took part in a discussion: it ran for the
-- discussion or wrote in it. A member's reply without a mention goes to it.
SELECT agent_id FROM (
    SELECT t.agent_id, t.created_at
    FROM agent_task_queue t
    WHERE t.doc_id = @doc_id AND t.doc_thread_id = @doc_thread_id
    UNION ALL
    SELECT c.author_id AS agent_id, c.created_at
    FROM doc_comment c
    WHERE c.workspace_id = @workspace_id AND c.doc_id = @doc_id
      AND (c.id = @doc_thread_id OR c.parent_id = @doc_thread_id)
      AND c.author_type = 'agent'
) participation
ORDER BY created_at DESC
LIMIT 1;

-- name: ListDocTasks :many
-- Runs on a doc for the discussions' run status, newest first: every run not
-- yet finished, however old, plus the 200 most recent finished ones.
SELECT * FROM agent_task_queue t
WHERE t.doc_id = $1
  AND (
    t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
    OR t.id IN (
      SELECT f.id FROM agent_task_queue f
      WHERE f.doc_id = $1
        AND f.status NOT IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
      ORDER BY f.created_at DESC
      LIMIT 200
    )
  )
ORDER BY t.created_at DESC;

-- name: CancelAgentTasksByDoc :many
-- Doc deletion cleanup: cancels every active run on the doc and returns the
-- rows so the caller can settle, broadcast and reconcile agent status.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL,
    cancelled_by_type = 'system', cancelled_by_id = NULL, cancelled_by_name = NULL
WHERE doc_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CancelAgentTasksByDocThread :many
-- Discussion deletion cleanup, the counterpart of CancelAgentTasksByDoc.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL,
    cancelled_by_type = 'system', cancelled_by_id = NULL, cancelled_by_name = NULL
WHERE doc_id = @doc_id AND doc_thread_id = @doc_thread_id
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: GetLastDocTaskSession :one
-- Resume context for an agent on a doc: one session and workdir per (doc,
-- agent), the doc analogue of GetLastTaskSession. Keep the per-session
-- reduction, retired-session exclusion and failure guards aligned with it.
WITH retired_sessions AS (
    SELECT DISTINCT r.retired_session_id AS session_id
    FROM agent_task_queue r
    WHERE r.agent_id = $1 AND r.doc_id = $2
      AND r.retired_session_id IS NOT NULL
), resume_overflow_at AS (
    SELECT MAX(COALESCE(t.completed_at, t.started_at, t.dispatched_at, t.created_at)) AS at
    FROM agent_task_queue t
    WHERE t.agent_id = $1 AND t.doc_id = $2
      AND t.status = 'failed'
      AND (
        COALESCE(t.failure_reason, '') = 'codex_resume_oversized'
        OR (COALESCE(t.error, '') ILIKE '%thread/resume failed%' AND COALESCE(t.error, '') ILIKE '%token too long%')
      )
), latest_per_session AS (
    SELECT DISTINCT ON (t.session_id)
        t.session_id, t.work_dir, t.runtime_id, t.status, t.failure_reason,
        COALESCE(t.error, '') AS error,
        t.started_at,
        COALESCE(t.completed_at, t.started_at, t.dispatched_at, t.created_at) AS terminal_at
    FROM agent_task_queue t
    WHERE t.agent_id = $1 AND t.doc_id = $2
      AND t.session_id IS NOT NULL
      AND t.status IN ('completed', 'failed', 'cancelled')
    ORDER BY t.session_id, COALESCE(t.completed_at, t.started_at, t.dispatched_at, t.created_at) DESC
)
SELECT session_id, work_dir, runtime_id, status, started_at FROM latest_per_session
WHERE session_id NOT IN (SELECT session_id FROM retired_sessions)
  AND (
    status IN ('completed', 'cancelled')
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow', 'codex_resume_oversized', 'opencode_clock_skew')
      AND NOT (error ILIKE '%400%' AND error ILIKE '%invalid_request_error%')
      AND NOT (error ILIKE '%image dimensions exceed max allowed size%' AND error ILIKE '%image.source.base64.data%')
      AND NOT (error ILIKE '%could not resolve authentication method%')
      AND NOT (error ~* 'must not be empty|must be non-?empty|must have non-?empty|non-?empty content|cannot be empty|should not be empty'
               AND error ~* 'role[^a-z0-9]{0,2}assistant|assistant message|message at position|messages\.[0-9]|messages\[[0-9]')
      AND NOT (error ILIKE '%invalid conversation history%')
    )
  )
  AND (
    (SELECT at FROM resume_overflow_at) IS NULL
    OR terminal_at > (SELECT at FROM resume_overflow_at)
  )
ORDER BY terminal_at DESC
LIMIT 1;

-- name: HasDocTaskReply :one
-- Whether a run already replied in its own discussion; a comment it left in
-- another discussion on the doc does not answer this one.
SELECT EXISTS (
    SELECT 1 FROM doc_comment
    WHERE workspace_id = @workspace_id AND doc_id = @doc_id AND source_task_id = @task_id
      AND (id = @doc_thread_id OR parent_id = @doc_thread_id)
)::bool;
