-- name: GetSystemWakeup :one
SELECT * FROM issue_wakeup WHERE issue_id= @issue_id AND system_rule= @system_rule;

-- name: CreateSystemWakeup :one
-- A platform rule on one issue. Two writers ensuring it at once keep the first
-- row; the loser reads it back with GetSystemWakeup.
INSERT INTO issue_wakeup(id,workspace_id,issue_id,instruction,kind,mode,event_types,timezone,condition,condition_state,enabled,system_rule)
VALUES(@id,@workspace_id,@issue_id,'','event','continuous',@event_types,'UTC',@condition,@condition_state,@enabled,@system_rule)
ON CONFLICT (issue_id,system_rule) WHERE system_rule IS NOT NULL DO NOTHING
RETURNING *;

-- name: CustomizeSystemWakeup :one
-- A person changed the rule on this issue; it stops following the workspace
-- default. Turning it on clears a platform pause.
UPDATE issue_wakeup SET enabled= @enabled,instruction= @instruction,customized_at=clock_timestamp(),
 paused_reason=CASE WHEN @enabled::bool THEN NULL ELSE paused_reason END,
 disabled_at=CASE WHEN @enabled::bool THEN NULL ELSE disabled_at END,
 updated_at=clock_timestamp()
WHERE id= @id RETURNING *;

-- name: ApplySystemWakeupDefault :many
-- The workspace default changed. Rules nobody customized and the platform did
-- not pause follow it.
UPDATE issue_wakeup SET enabled= @enabled,updated_at=clock_timestamp()
WHERE workspace_id= @workspace_id AND system_rule= @system_rule AND customized_at IS NULL
 AND paused_reason IS NULL AND enabled<> @enabled::bool
RETURNING *;

-- name: CountCustomizedSystemWakeups :one
-- Open issues whose rule a person changed, for the workspace settings page.
SELECT count(*) FROM issue_wakeup w JOIN issue i ON i.id=w.issue_id AND i.workspace_id=w.workspace_id
WHERE w.workspace_id= @workspace_id AND w.system_rule= @system_rule AND w.customized_at IS NOT NULL
 AND i.status NOT IN ('done','cancelled')
 AND NOT EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed'));

-- name: MergeWorkspaceSettings :exec
UPDATE workspace SET settings=COALESCE(settings,'{}'::jsonb) || @patch::jsonb,updated_at=now() WHERE id= @id;

-- name: ListParentsWithoutSystemWakeup :many
-- Open parents that predate their rule, for the one-time backfill.
SELECT p.* FROM (SELECT DISTINCT c.parent_issue_id AS id FROM issue c WHERE c.parent_issue_id IS NOT NULL) parents
JOIN issue p ON p.id=parents.id
WHERE NOT EXISTS(SELECT 1 FROM issue_wakeup w WHERE w.issue_id=p.id AND w.system_rule= @system_rule)
 AND p.status NOT IN ('done','cancelled')
 AND NOT EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=p.workspace_id AND s.key=p.status AND s.category IN ('done','closed'))
LIMIT @page_limit;

-- name: ListChildConditionWakeups :many
-- Enabled rules on a parent whose condition reads its sub-issues.
SELECT * FROM issue_wakeup WHERE issue_id= @issue_id AND enabled AND condition->>'type'='children_done'
ORDER BY (system_rule IS NOT NULL),id;

-- name: ClaimChildEvents :many
-- Claim a parent's recorded sub-issue changes. A claim that was not finished
-- within five minutes (a crashed or failed attempt) can be claimed again.
UPDATE issue_child_event SET claimed_at=clock_timestamp()
WHERE parent_id= @parent_id AND processed_at IS NULL
 AND (claimed_at IS NULL OR claimed_at < clock_timestamp()-interval '5 minutes')
RETURNING *;

-- name: FinishChildEvents :exec
UPDATE issue_child_event SET processed_at=clock_timestamp() WHERE id=ANY(@ids::uuid[]);

-- name: ListStaleChildEventParents :many
-- Parents whose changes were not processed right after their write.
SELECT DISTINCT parent_id FROM issue_child_event
WHERE processed_at IS NULL
 AND ((claimed_at IS NULL AND created_at < clock_timestamp()-interval '30 seconds') OR claimed_at < clock_timestamp()-interval '5 minutes')
LIMIT 50;

-- name: DeleteProcessedChildEvents :execrows
WITH batch AS MATERIALIZED (
 SELECT e.id FROM issue_child_event e WHERE e.processed_at < @cutoff
 ORDER BY e.processed_at LIMIT 1000 FOR UPDATE SKIP LOCKED
)
DELETE FROM issue_child_event d USING batch WHERE d.id=batch.id;
