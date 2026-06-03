-- name: ListSkillsForArchive :many
-- FIR-2656 nightly skill archive sync. Returns every skill's current
-- approved state across all workspaces. The skill row always holds the
-- merged/approved content (open change requests live in
-- skill_change_request and never touch skill.content), so reading skill
-- directly is exactly "the approved versions, no open drafts".
SELECT id, workspace_id, name, description, content, current_version, config
FROM skill
ORDER BY workspace_id, name;
