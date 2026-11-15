-- MUL-9284: a private project is now visible to its member list only. A
-- workspace owner or admin no longer sees one through their role. Two
-- backfills keep existing private projects exactly as reachable as they were:
--
-- 1. Everyone who could see a private project through their role becomes a
--    member of it. Nobody loses access the moment the role rule goes away,
--    and the member list now shows everyone who can see the project; the lead
--    removes whoever should not stay.
-- 2. A private project is managed by its lead, so it needs a human one. A
--    project with no lead, an agent lead, or a lead who is no longer a member
--    gets its creator, or failing that its longest-standing member.
--
-- Data only and idempotent, so servers still on the old rule keep working.

INSERT INTO project_member (workspace_id, project_id, user_id)
SELECT p.workspace_id, p.id, m.user_id
FROM project p
JOIN member m ON m.workspace_id = p.workspace_id AND m.role IN ('owner', 'admin')
WHERE p.visibility = 'private'
ON CONFLICT (project_id, user_id) DO NOTHING;

UPDATE project p
SET lead_type = 'member', lead_id = pick.user_id, updated_at = now()
FROM (
    SELECT DISTINCT ON (pm.project_id) pm.project_id, pm.user_id
    FROM project_member pm
    JOIN project pr ON pr.id = pm.project_id
    JOIN member m ON m.workspace_id = pr.workspace_id AND m.user_id = pm.user_id
    WHERE pr.visibility = 'private'
    ORDER BY pm.project_id,
             (pr.creator_type = 'member' AND pr.creator_id = pm.user_id) DESC NULLS LAST,
             pm.created_at ASC,
             CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
             m.created_at ASC,
             pm.user_id ASC
) pick
WHERE p.id = pick.project_id
  AND p.visibility = 'private'
  AND NOT EXISTS (
      SELECT 1 FROM project_member lm
      WHERE p.lead_type = 'member' AND lm.project_id = p.id AND lm.user_id = p.lead_id
  );
