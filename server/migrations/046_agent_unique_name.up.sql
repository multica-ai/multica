-- Step 1: delete duplicates, keep the most recently updated one
DELETE FROM agent a
USING (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY workspace_id, name ORDER BY updated_at DESC) AS rn
    FROM agent
) ranked
WHERE a.id = ranked.id AND ranked.rn > 1;

-- Step 2: add the constraint
ALTER TABLE agent
    ADD CONSTRAINT agent_workspace_name_unique UNIQUE (workspace_id, name);
