-- name: ListAgentTaskHistoryPage :many
-- Fixed-size keyset reads for the backwards-compatible full-history response.
-- Do not read execution context, MCP overlays, or issue snapshots into Go.
SELECT t.id,
    t.agent_id,
    t.issue_id,
    t.status,
    t.priority,
    t.dispatched_at,
    t.started_at,
    t.completed_at,
    t.result,
    t.error,
    t.created_at,
    t.runtime_id,
    t.work_dir,
    t.trigger_comment_id,
    t.chat_session_id,
    t.autopilot_run_id,
    t.attempt,
    t.max_attempts,
    t.parent_task_id,
    t.failure_reason,
    t.trigger_summary,
    t.is_leader_task,
    t.handoff_note,
    t.originator_user_id,
    t.coalesced_comment_ids,
    t.delivered_comment_ids,
    t.originator_source,
    t.delegated_from_task_id,
    t.retry_of_task_id,
    t.rerun_of_task_id,
    t.rule_version_id,
    t.trigger_evidence_kind,
    t.trigger_evidence_ref_id,
    t.accountable_user_id,
    t.branch_name,
    t.durable_work_dir,
    t.cancelled_by_type,
    t.cancelled_by_id,
    t.cancelled_by_name,
    jsonb_build_object(
        'type', t.context->'type',
        'wakeup_id', t.context->'wakeup_id',
        'comment_change_cancelled_task_id', t.context->'comment_change_cancelled_task_id'
    )::jsonb AS context
FROM agent_task_queue t
WHERE t.agent_id = @owner_id
  AND (t.created_at, t.id) < (
      COALESCE(@before_created_at::timestamptz, 'infinity'::timestamptz),
      COALESCE(@before_id::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
  AND NOT (t.escalation_for_task_id IS NOT NULL AND t.started_at IS NULL AND t.status IN ('deferred', 'cancelled'))
ORDER BY t.created_at DESC, t.id DESC
LIMIT @page_size;

-- name: ListIssueTaskHistoryPage :many
-- Fixed-size keyset reads for the backwards-compatible full-history response.
-- Do not read execution context, MCP overlays, or issue snapshots into Go.
SELECT t.id,
    t.agent_id,
    t.issue_id,
    t.status,
    t.priority,
    t.dispatched_at,
    t.started_at,
    t.completed_at,
    t.result,
    t.error,
    t.created_at,
    t.runtime_id,
    t.work_dir,
    t.trigger_comment_id,
    t.chat_session_id,
    t.autopilot_run_id,
    t.attempt,
    t.max_attempts,
    t.parent_task_id,
    t.failure_reason,
    t.trigger_summary,
    t.is_leader_task,
    t.handoff_note,
    t.originator_user_id,
    t.coalesced_comment_ids,
    t.delivered_comment_ids,
    t.originator_source,
    t.delegated_from_task_id,
    t.retry_of_task_id,
    t.rerun_of_task_id,
    t.rule_version_id,
    t.trigger_evidence_kind,
    t.trigger_evidence_ref_id,
    t.accountable_user_id,
    t.branch_name,
    t.durable_work_dir,
    t.cancelled_by_type,
    t.cancelled_by_id,
    t.cancelled_by_name,
    jsonb_build_object(
        'type', t.context->'type',
        'wakeup_id', t.context->'wakeup_id',
        'comment_change_cancelled_task_id', t.context->'comment_change_cancelled_task_id'
    )::jsonb AS context
FROM agent_task_queue t
WHERE t.issue_id = @owner_id
  AND (t.created_at, t.id) < (
      COALESCE(@before_created_at::timestamptz, 'infinity'::timestamptz),
      COALESCE(@before_id::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid))
  AND NOT (t.escalation_for_task_id IS NOT NULL AND t.started_at IS NULL AND t.status IN ('deferred', 'cancelled'))
  AND (NOT @active_only::boolean OR t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory'))
ORDER BY t.created_at DESC, t.id DESC
LIMIT @page_size;
