-- MUL-9284: a private project is now visible to its member list only. A
-- workspace owner or admin no longer sees one through their role.
--
-- Member lists are left as they are. Whoever made a project private and its
-- member lead were put on the list at that moment, so nobody has to be added
-- for a project to stay reachable; an owner or admin who relied on their role
-- stops seeing it, which is the point of the change.
--
-- A private project is managed by its lead, so it needs a human one. A
-- project with no lead, an agent lead, or a lead who is no longer a member
-- gets its creator, or failing that its longest-standing member. One with no
-- member left keeps what it has until a workspace owner takes it over.
--
-- Data only and idempotent, so servers still on the old rule keep working.

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
