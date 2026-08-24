-- Skill Usage Event queries (Plan B async statistics)

-- name: UpsertSkillUsageEvent :exec
INSERT INTO skill_usage_event (skill_id, task_id, issue_id, workspace_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (skill_id, task_id) DO NOTHING;

-- name: CountSkillUsageEvents :one
SELECT COUNT(*) FROM skill_usage_event
WHERE skill_id = $1;

-- name: ListSkillUsageEventsBySkill :many
SELECT * FROM skill_usage_event
WHERE skill_id = $1
ORDER BY created_at DESC;

-- name: CountSkillUsageEventsByIssue :one
SELECT COUNT(*) FROM skill_usage_event
WHERE skill_id = $1 AND issue_id = $2;

-- name: ListUnprocessedCompletedTasks :many
SELECT atq.id, atq.runtime_id, atq.issue_id, ar.workspace_id, ar.provider
FROM agent_task_queue atq
JOIN agent_runtime ar ON atq.runtime_id = ar.id
WHERE atq.status IN ('completed', 'failed')
  AND atq.completed_at > $1
ORDER BY atq.completed_at
LIMIT $2;

-- name: GetSkillUsageProcessCursor :one
SELECT last_completed_at FROM skill_usage_process_cursor WHERE id = 1;

-- name: UpdateSkillUsageProcessCursor :exec
UPDATE skill_usage_process_cursor SET last_completed_at = $1, updated_at = now() WHERE id = 1;
