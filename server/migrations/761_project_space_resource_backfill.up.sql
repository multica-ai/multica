INSERT INTO project_resource (
    project_id,
    workspace_id,
    resource_type,
    resource_ref,
    label,
    position,
    created_by
)
SELECT
    p.id,
    p.workspace_id,
    'project_space',
    '{"version":1}'::jsonb,
    'Project space',
    -100,
    NULL
FROM project p
WHERE NOT EXISTS (
    SELECT 1
    FROM project_resource pr
    WHERE pr.project_id = p.id
      AND pr.resource_type = 'project_space'
);
