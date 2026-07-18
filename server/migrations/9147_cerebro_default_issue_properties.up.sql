-- Backfill the two commercial number fields for every existing workspace.
WITH defaults(name, description, position_offset) AS (
    VALUES
        ('Business value (DKK)', 'Expected business value in Danish kroner.', 1),
        ('Effort (DKK)', 'Expected delivery effort in Danish kroner.', 2)
), workspace_positions AS (
    SELECT w.id AS workspace_id, COALESCE(MAX(p.position), 0) AS max_position
    FROM workspace w
    LEFT JOIN issue_property p ON p.workspace_id = w.id
    GROUP BY w.id
)
INSERT INTO issue_property (workspace_id, name, type, description, icon, config, position)
SELECT positions.workspace_id,
       defaults.name,
       'number',
       defaults.description,
       '',
       '{}'::jsonb,
       positions.max_position + defaults.position_offset
FROM workspace_positions positions
CROSS JOIN defaults
WHERE NOT EXISTS (
    SELECT 1
    FROM issue_property existing
    WHERE existing.workspace_id = positions.workspace_id
      AND LOWER(existing.name) = LOWER(defaults.name)
)
ON CONFLICT DO NOTHING;
