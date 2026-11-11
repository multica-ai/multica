-- name: ListIssueBlockers :many
SELECT b.issue_id,i.id,i.number,i.title,i.status,i.project_id,COALESCE(p.visibility='private',false)::boolean AS private,b.via,
 (i.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed'))) AS closed
FROM effective_issue_blocker b JOIN issue i ON i.id=b.blocker_issue_id AND i.workspace_id=b.workspace_id
LEFT JOIN project p ON p.id=i.project_id AND p.workspace_id=i.workspace_id
WHERE b.workspace_id = sqlc.arg('workspace_id') AND b.issue_id=ANY(sqlc.arg('issue_ids')::uuid[])
ORDER BY b.issue_id,b.via,i.number;

-- name: HasOpenIssueBlockers :one
SELECT issue_has_open_blockers(sqlc.arg('workspace_id')::uuid,sqlc.arg('issue_id')::uuid)::boolean AS blocked;

-- name: GetIssueRunAdmission :one
SELECT triage_state,issue_has_open_blockers(workspace_id,id)::boolean AS blocked
FROM issue WHERE id = sqlc.arg('issue_id');

-- name: LockIssueBlockerTarget :one
-- Keep deletion from passing between reference validation and the edge write.
SELECT id FROM issue WHERE workspace_id = sqlc.arg('workspace_id') AND id = sqlc.arg('issue_id') FOR KEY SHARE;

-- name: LockIssueBlockerGraph :exec
SELECT pg_advisory_xact_lock(hashtextextended('issue-blockers:' || sqlc.arg('workspace_id')::uuid::text,0));

-- name: AddIssueBlocker :exec
INSERT INTO issue_blocker(workspace_id,issue_id,blocker_issue_id,created_by_type,created_by_id)
VALUES(sqlc.arg('workspace_id'),sqlc.arg('issue_id'),sqlc.arg('blocker_issue_id'),sqlc.arg('created_by_type'),sqlc.arg('created_by_id'))
ON CONFLICT DO NOTHING;

-- name: RemoveIssueBlocker :exec
DELETE FROM issue_blocker WHERE workspace_id = sqlc.arg('workspace_id') AND issue_id = sqlc.arg('issue_id') AND blocker_issue_id = sqlc.arg('blocker_issue_id');

-- name: AcknowledgeIssueBlockerRelease :exec
-- Ordinary admission and dependency dispatch consume the same ready state.
-- Run in the transaction that inserted the task, before notifying its runtime.
WITH acknowledged AS (
 UPDATE issue_wakeup w
 SET condition_state='ready',next_fire_at=NULL,last_task_id=t.id,updated_at=clock_timestamp()
 FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id
 WHERE t.id=sqlc.arg('task_id') AND w.issue_id=i.id AND w.workspace_id=i.workspace_id
 AND w.system_rule='blockers_done' AND w.enabled
 AND i.status<>'backlog' AND i.triage_state IS NULL
 AND ((i.assignee_type='agent' AND i.assignee_id=t.agent_id)
   OR (i.assignee_type='squad' AND i.assignee_id=t.squad_id AND t.is_leader_task))
 AND NOT issue_has_open_blockers(i.workspace_id,i.id)
 RETURNING w.id,w.revision
)
UPDATE issue_wakeup_receipt r SET processed_at=clock_timestamp(),task_id=sqlc.arg('task_id')
FROM acknowledged a WHERE r.wakeup_id=a.id AND r.revision=a.revision AND r.processed_at IS NULL;
