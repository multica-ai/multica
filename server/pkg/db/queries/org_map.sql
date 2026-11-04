-- The org map (`GET /api/company/org-map`): floors with their chiefs, squads with
-- their leader, charter, project, workflow and open work, and squad members with
-- their skills. Every query filters by workspace_id; there are no foreign keys.

-- name: ListOrgMapFloorChiefs :many
SELECT
    fc.floor_id,
    fc.agent_id,
    COALESCE(a.name, '')::text AS agent_name,
    (a.archived_at IS NOT NULL)::bool AS agent_archived,
    COALESCE(rt.status, '')::text AS runtime_status
FROM floor_chief fc
LEFT JOIN agent a ON a.id = fc.agent_id AND a.workspace_id = fc.workspace_id
LEFT JOIN agent_runtime rt ON rt.id = a.runtime_id
WHERE fc.workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListOrgMapSquads :many
-- Live squads, floor order first, then seat order. The caller passes row_limit
-- one above the cap so it can tell that the list was cut short.
SELECT
    s.id AS squad_id,
    s.name,
    s.floor_id,
    COALESCE(f.name, '')::text AS floor_name,
    s.leader_id,
    COALESCE(la.name, '')::text AS leader_name,
    (la.archived_at IS NOT NULL)::bool AS leader_archived,
    COALESCE(lrt.status, '')::text AS leader_runtime_status,
    COALESCE(c.mission, '')::text AS mission,
    COALESCE(c.intake, '')::text AS intake,
    COALESCE(c.response_hours, 48)::integer AS response_hours,
    (c.squad_id IS NOT NULL)::bool AS has_charter,
    pr.id AS project_id,
    COALESCE(pr.title, '')::text AS project_title,
    pr.workflow_id,
    COALESCE(wf.name, '')::text AS workflow_name,
    (SELECT count(*) FROM squad_member sm WHERE sm.squad_id = s.id)::bigint AS member_count,
    (
        SELECT count(*) FROM issue i
        WHERE i.workspace_id = s.workspace_id
          AND (
            (i.assignee_type = 'squad' AND i.assignee_id = s.id)
            OR (pr.id IS NOT NULL AND i.project_id = pr.id)
          )
          AND NOT (
            i.status IN ('done', 'cancelled')
            OR EXISTS (
              SELECT 1 FROM issue_status st
              WHERE st.workspace_id = i.workspace_id
                AND st.key = i.status
                AND st.category IN ('done', 'closed')
            )
          )
    )::bigint AS open_issues
FROM squad s
LEFT JOIN squad_charter c ON c.squad_id = s.id AND c.workspace_id = s.workspace_id
LEFT JOIN company_floor f ON f.id = s.floor_id AND f.workspace_id = s.workspace_id
LEFT JOIN agent la ON la.id = s.leader_id AND la.workspace_id = s.workspace_id
LEFT JOIN agent_runtime lrt ON lrt.id = la.runtime_id
LEFT JOIN LATERAL (
    SELECT p.id, p.title, p.workflow_id
    FROM project p
    WHERE p.workspace_id = s.workspace_id AND p.squad_id = s.id
    ORDER BY p.created_at ASC, p.id ASC
    LIMIT 1
) pr ON true
LEFT JOIN issue_workflow wf ON wf.id = pr.workflow_id AND wf.workspace_id = s.workspace_id
WHERE s.workspace_id = sqlc.arg('workspace_id')::uuid
  AND s.archived_at IS NULL
  AND (sqlc.narg('floor_id')::uuid IS NULL OR s.floor_id = sqlc.narg('floor_id')::uuid)
ORDER BY COALESCE(f.position, 2147483647) ASC, s.company_slot ASC NULLS LAST, s.created_at ASC, s.id ASC
LIMIT sqlc.arg('row_limit')::integer;

-- name: ListOrgMapSquadMembers :many
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role,
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id)::bool AS is_leader,
    COALESCE(CASE WHEN sm.member_type = 'agent' THEN a.name ELSE u.name END, '')::text AS name,
    (sm.member_type = 'agent' AND a.archived_at IS NOT NULL)::bool AS archived
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id AND s.workspace_id = sqlc.arg('workspace_id')::uuid
LEFT JOIN agent a ON sm.member_type = 'agent' AND a.id = sm.member_id AND a.workspace_id = s.workspace_id
LEFT JOIN "user" u ON sm.member_type = 'member' AND u.id = sm.member_id
WHERE sm.squad_id = ANY(sqlc.arg('squad_ids')::uuid[])
ORDER BY sm.squad_id ASC, is_leader DESC, sm.created_at ASC, sm.id ASC;

-- name: ListOrgMapAgentSkills :many
-- Enabled skills of the given agents, by name. The skill junction is the same
-- predicate ListAgentSkills uses.
SELECT ask.agent_id, s.name
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = sqlc.arg('workspace_id')::uuid
  AND ask.enabled = TRUE
  AND ask.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
ORDER BY s.name ASC;
