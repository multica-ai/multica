-- Skill automation link queries — sync triggered_by metadata and impact analysis.
-- Part of TECH-3077 skill library unlock.

-- name: UpsertSkillAutomationLink :exec
INSERT INTO skill_automation_link (skill_id, automation_type, automation_id, automation_name, schedule)
VALUES (@skill_id, @automation_type, @automation_id, @automation_name, @schedule)
ON CONFLICT (skill_id, automation_type, automation_name)
DO UPDATE SET
    automation_id = EXCLUDED.automation_id,
    schedule = EXCLUDED.schedule,
    updated_at = now();

-- name: DeleteSkillAutomationLinks :exec
DELETE FROM skill_automation_link WHERE skill_id = @skill_id;

-- name: ListSkillAutomationLinks :many
SELECT id, skill_id, automation_type, automation_id, automation_name, schedule, created_at, updated_at
FROM skill_automation_link
WHERE skill_id = @skill_id
ORDER BY automation_type, automation_name;

-- name: ListSkillsByAutomation :many
SELECT DISTINCT s.id, s.name, s.description, s.current_version, s.metadata
FROM skill s
JOIN skill_automation_link sal ON sal.skill_id = s.id
WHERE sal.automation_id = @automation_id AND s.workspace_id = @workspace_id;

-- name: CountAutomationLinksForSkill :one
SELECT COUNT(*) FROM skill_automation_link WHERE skill_id = @skill_id;
