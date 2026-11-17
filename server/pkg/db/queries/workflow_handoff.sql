-- name: LockWorkflowSquadHandoff :exec
-- Serialize admission for one issue without taking its owner-row locks out of
-- the order lock_task_owner_rows uses to fence workspace teardown.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('issue_id')::uuid::text || ':workflow_squad_handoff', 0));

-- name: GetWorkflowSquadHandoffTask :one
-- A replay of the same status write is already covered, including after claim
-- or completion. A later visit to the same stage has a different issue revision.
SELECT id FROM agent_task_queue
WHERE issue_id = @issue_id
  AND agent_id = @agent_id
  AND squad_id = @squad_id
  AND is_leader_task
  AND context->>'workflow_handoff_revision' = sqlc.arg('issue_revision')::bigint::text
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListAssigneeRunsForHandoff :many
-- Preview and explicit stop must cover every unfinished run of the previous
-- assignee, including stage handoffs still waiting for a pending queue slot.
SELECT id, agent_id, status, started_at, created_at
FROM agent_task_queue
WHERE issue_id = @issue_id
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  AND (
    (sqlc.arg('assignee_type')::text = 'agent' AND agent_id = @assignee_id)
    OR (sqlc.arg('assignee_type')::text = 'squad' AND squad_id = @assignee_id)
  )
ORDER BY created_at DESC, id DESC;
