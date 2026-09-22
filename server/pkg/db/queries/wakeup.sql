-- name: CreateIssueWakeup :one
INSERT INTO issue_wakeup(id,workspace_id,issue_id,agent_id,created_by,source_task_id,parent_comment_id,instruction,kind,mode,event_types,filter_agent_id,filter_task_id,filter_actor_type,filter_actor_id,interval_seconds,cron_expression,timezone,next_fire_at)
VALUES(@id,@workspace_id,@issue_id,@agent_id,@created_by,sqlc.narg(source_task_id),sqlc.narg(parent_comment_id),@instruction,@kind,@mode,@event_types,sqlc.narg(filter_agent_id),sqlc.narg(filter_task_id),sqlc.narg(filter_actor_type),sqlc.narg(filter_actor_id),sqlc.narg(interval_seconds),sqlc.narg(cron_expression),@timezone,sqlc.narg(next_fire_at)) RETURNING *;
-- name: ListIssueWakeups :many
SELECT w.id,w.workspace_id,w.issue_id,w.agent_id,w.created_by,w.source_task_id,w.parent_comment_id,w.instruction,
 w.kind,w.mode,w.event_types,w.filter_actor_type,
 (CASE WHEN actor_agent.id IS NOT NULL OR actor_member.user_id IS NOT NULL THEN w.filter_actor_id END)::uuid AS filter_actor_id,
 COALESCE(actor_agent.name,actor_user.name,'')::text AS filter_actor_name,
 (CASE WHEN source.id IS NOT NULL THEN w.filter_agent_id END)::uuid AS filter_agent_id,
 (CASE WHEN EXISTS(SELECT 1 FROM agent_task_queue ft JOIN agent fa ON fa.id=ft.agent_id AND fa.workspace_id=w.workspace_id
  WHERE ft.id=w.filter_task_id AND ft.issue_id=w.issue_id AND fa.id=ANY(@agent_ids::uuid[])) THEN w.filter_task_id END)::uuid AS filter_task_id,
 w.interval_seconds,w.cron_expression,w.timezone,w.next_fire_at,w.enabled,w.disabled_at,w.revision,
 w.last_task_id,w.last_error,w.created_at,w.updated_at,a.name AS agent_name,source.name AS filter_agent_name,t.status AS last_task_status
FROM issue_wakeup w JOIN agent a ON a.id=w.agent_id AND a.workspace_id=w.workspace_id
LEFT JOIN agent actor_agent ON w.filter_actor_type='agent' AND actor_agent.id=w.filter_actor_id AND actor_agent.workspace_id=w.workspace_id AND actor_agent.id=ANY(@agent_ids::uuid[])
LEFT JOIN member actor_member ON w.filter_actor_type='member' AND actor_member.user_id=w.filter_actor_id AND actor_member.workspace_id=w.workspace_id
LEFT JOIN "user" actor_user ON actor_user.id=actor_member.user_id
LEFT JOIN agent source ON source.id=w.filter_agent_id AND source.workspace_id=w.workspace_id AND source.id=ANY(@agent_ids::uuid[])
LEFT JOIN agent_task_queue t ON t.id=w.last_task_id AND t.issue_id=w.issue_id AND t.agent_id=w.agent_id
WHERE w.workspace_id= @workspace_id AND w.issue_id= @issue_id ORDER BY w.created_at,w.id;

-- name: ListWorkspaceWakeupSummaryRows :many
-- No prompts/history; at most three previews per issue plus exact counts.
WITH ranked AS (
 SELECT w.issue_id,w.id,w.agent_id,a.name AS agent_name,w.kind,w.mode,w.event_types,w.filter_actor_type,
 (CASE WHEN actor_agent.id IS NOT NULL OR actor_member.user_id IS NOT NULL THEN w.filter_actor_id END)::uuid AS filter_actor_id,
 COALESCE(actor_agent.name,actor_user.name,'')::text AS filter_actor_name,
  (CASE WHEN EXISTS(SELECT 1 FROM agent_task_queue ft JOIN agent fa ON fa.id=ft.agent_id AND fa.workspace_id=w.workspace_id
   WHERE ft.id=w.filter_task_id AND ft.issue_id=w.issue_id AND fa.id=ANY(@agent_ids::uuid[])) THEN w.filter_task_id END)::uuid AS filter_task_id,
  source.name AS filter_agent_name,w.interval_seconds,w.cron_expression,w.timezone,w.next_fire_at,
  count(*) OVER(PARTITION BY w.issue_id) AS active_count,
  count(*) FILTER(WHERE w.kind='event') OVER(PARTITION BY w.issue_id) AS event_count,
  row_number() OVER(PARTITION BY w.issue_id ORDER BY w.next_fire_at NULLS LAST,w.created_at,w.id) AS rank
 FROM issue_wakeup w
 JOIN issue i ON i.id=w.issue_id AND i.workspace_id=w.workspace_id
 JOIN agent a ON a.id=w.agent_id AND a.workspace_id=w.workspace_id
 LEFT JOIN agent actor_agent ON w.filter_actor_type='agent' AND actor_agent.id=w.filter_actor_id AND actor_agent.workspace_id=w.workspace_id AND actor_agent.id=ANY(@agent_ids::uuid[])
LEFT JOIN member actor_member ON w.filter_actor_type='member' AND actor_member.user_id=w.filter_actor_id AND actor_member.workspace_id=w.workspace_id
LEFT JOIN "user" actor_user ON actor_user.id=actor_member.user_id
LEFT JOIN agent source ON source.id=w.filter_agent_id AND source.workspace_id=w.workspace_id AND source.id=ANY(@agent_ids::uuid[])
 WHERE w.workspace_id= @workspace_id AND w.enabled
  AND i.status NOT IN ('done','cancelled')
  AND NOT EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed'))
)
SELECT issue_id,id,agent_id,agent_name,kind,mode,event_types,filter_actor_type,filter_actor_id,filter_actor_name,filter_task_id,filter_agent_name,interval_seconds,cron_expression,timezone,next_fire_at,active_count,event_count
FROM ranked WHERE rank<=3 ORDER BY issue_id,rank;
-- name: GetIssueWakeup :one
SELECT * FROM issue_wakeup WHERE id= @id AND workspace_id= @workspace_id;
-- name: LockIssueWakeup :one
SELECT * FROM issue_wakeup WHERE id= @id FOR UPDATE;
-- name: LockWakeupIssue :one
SELECT * FROM issue WHERE id= @id FOR NO KEY UPDATE;
-- name: LockWakeupSourceTask :one
SELECT * FROM agent_task_queue WHERE id= @id AND issue_id= @issue_id FOR UPDATE NOWAIT;
-- name: CancelUnstartedWakeupTasks :many
UPDATE agent_task_queue SET status='cancelled',completed_at=now(),error='Wakeup disabled or updated'
WHERE context->>'wakeup_id'= @wakeup_id::text AND status IN ('queued','deferred') AND started_at IS NULL RETURNING *;
-- name: DisableIssueWakeups :exec
-- Closing wins over every subscription on the issue; reopening does not rearm.
UPDATE issue_wakeup SET enabled=false,disabled_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE issue_id= @issue_id AND disabled_at IS NULL;
-- name: CancelUnstartedIssueWakeupTasks :many
UPDATE agent_task_queue SET status='cancelled',completed_at=now(),error='Issue closed; wakeup disabled'
WHERE issue_id= @issue_id AND context->>'wakeup_id' IS NOT NULL AND status IN ('queued','deferred') AND started_at IS NULL RETURNING *;
-- name: ListReadyWakeups :many
WITH candidates AS (
 SELECT id FROM issue_wakeup WHERE enabled AND kind<>'event' AND next_fire_at<=now()
 UNION
 SELECT wakeup_id FROM issue_wakeup_receipt WHERE processed_at IS NULL
)
SELECT w.* FROM candidates c JOIN issue_wakeup w ON w.id=c.id
ORDER BY w.updated_at,w.id LIMIT 100;
-- name: ListPendingWakeupReceipts :many
SELECT * FROM issue_wakeup_receipt WHERE wakeup_id= @wakeup_id AND revision= @revision AND processed_at IS NULL ORDER BY created_at,id LIMIT 100 FOR UPDATE;

-- name: DeleteExpiredWakeupReceipts :execrows
-- Pending inputs are never expired. Bound work and avoid waiting on dispatch.
-- MATERIALIZED evaluates the batch once. As an IN subquery, a nested-loop plan
-- rescans it per outer row, skips the rows this DELETE already removed, and
-- slides the LIMIT window until every expired row is gone.
WITH batch AS MATERIALIZED (
 SELECT expired.id FROM issue_wakeup_receipt expired WHERE expired.processed_at < @cutoff
 ORDER BY expired.processed_at,expired.id LIMIT 1000 FOR UPDATE SKIP LOCKED
)
DELETE FROM issue_wakeup_receipt r USING batch WHERE r.id=batch.id;
-- name: RecordWakeupReceipt :one
INSERT INTO issue_wakeup_receipt(id,wakeup_id,revision,event_key,event_type,payload)
VALUES(@id,@wakeup_id,@revision,@event_key,@event_type,@payload)
ON CONFLICT(wakeup_id,revision,event_key) DO UPDATE SET event_key=EXCLUDED.event_key RETURNING *;
-- name: ConsumeWakeupReceipts :exec
UPDATE issue_wakeup_receipt SET task_id=sqlc.narg(task_id),processed_at=now() WHERE id=ANY(@ids::uuid[]);
-- name: DiscardWakeupReceipts :exec
UPDATE issue_wakeup_receipt SET processed_at=now() WHERE wakeup_id= @id AND processed_at IS NULL;
-- name: AdvanceIssueWakeup :exec
UPDATE issue_wakeup SET enabled= @enabled,next_fire_at=sqlc.narg(next_fire_at),last_task_id=COALESCE(sqlc.narg(last_task_id),last_task_id),last_error=sqlc.narg(last_error),updated_at=clock_timestamp() WHERE id= @id;
-- name: FindPendingWakeupTask :one
SELECT * FROM agent_task_queue WHERE context->>'wakeup_id'= @wakeup_id::text AND status IN ('queued','dispatched') ORDER BY created_at LIMIT 1 FOR UPDATE;

-- name: CreateWakeupTask :one
-- Fenced against workspace teardown: lock_task_owner_rows (migration 284)
-- locks the owners' workspace rows in the writer's own transaction and returns
-- false once they are gone, so this statement writes no row instead of stranding
-- a task in a workspace that has just been deleted (MUL-5999).
-- head_sha stamps the commit under review into the task's context JSONB so the
-- reviewer-loop dedup (HasPendingTaskForIssueAndAgent) can tell a pending run
-- against an OLD head apart from a fresh request against a NEW head (TEN-356).
-- Empty/absent head_sha leaves context NULL, preserving pre-TEN-356 behavior for
-- issues with no linked PR. Issue-linked tasks never hit quick-create context
-- parsing (parseQuickCreateContext short-circuits on IssueID.Valid), so this
-- key rides harmlessly alongside.
-- id is minted by the application as a UUIDv7 (pkg/dbid) so consecutive
-- enqueues cluster in a narrow contiguous primary-key range instead of
-- scattering across the B-tree. On a table with existing v4 ids, that range
-- is not necessarily the tree's right edge.
-- COALESCE keeps the column's gen_random_uuid() default reachable, so a caller
-- that passes no id still inserts — it just gets a random v4, exactly as before.
-- The same pattern is used by every INSERT listed in pkg/dbid's write table.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
    coalesced_comment_ids, trigger_summary, force_fresh_session, is_leader_task, handoff_note,
    squad_id, context, originator_user_id, accountable_user_id, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, delegated_from_task_id, rule_version_id, rerun_of_task_id, trigger_evidence_kind, trigger_evidence_ref_id,
    id
)
SELECT
    $1, $2, $3, 'queued', $4, sqlc.narg(trigger_comment_id),
    COALESCE(sqlc.narg(coalesced_comment_ids)::uuid[], '{}'),
    sqlc.narg(trigger_summary),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    COALESCE(sqlc.narg('is_leader_task')::boolean, FALSE),
    sqlc.narg(handoff_note),
    sqlc.narg(squad_id),
    CASE
        WHEN COALESCE(sqlc.narg('head_sha')::text, '') <> ''
        THEN jsonb_build_object('head_sha', sqlc.narg('head_sha')::text)
        ELSE '{}'::jsonb
    END || @wakeup_context::jsonb,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(delegated_from_task_id),
    sqlc.narg(rule_version_id),
    sqlc.narg(rerun_of_task_id),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id),
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
WHERE lock_task_owner_rows($1, $3, $2)
RETURNING *;

-- name: LocklessWakeup :one
SELECT * FROM issue_wakeup WHERE id= @id;

-- name: ReplaceWakeupEvidence :one
UPDATE agent_task_queue SET handoff_note=sqlc.narg(handoff_note), context=COALESCE(context,'{}'::jsonb) || jsonb_build_object('wakeup_evidence', @wakeup_evidence::jsonb) WHERE id= @id AND status='queued' RETURNING *;
-- name: NoteWakeupFailure :exec
UPDATE issue_wakeup SET last_error=sqlc.narg(last_error),updated_at=clock_timestamp() WHERE id= @id;
-- name: TouchWakeupDispatch :exec
-- Move blocked configurations to the back of the scan so one noisy issue
-- cannot monopolize the bounded batch.
UPDATE issue_wakeup SET updated_at=clock_timestamp() WHERE id= @id;
