-- 124_seed_builtin_agents.down.sql
-- Remove the seeded global built-in agents. Scoped to the exact seeded ids so
-- this never deletes built-ins an admin promoted manually after deploy.

DELETE FROM multica_agent WHERE id IN (
    'dd0683f4-d72c-4b49-8030-827f5b15df2e',
    '5e2fccac-6257-4ea5-ac7a-a5d8a4765917',
    '4348e20d-eadc-4095-ac7a-cd480e927375',
    'c0bea924-c78f-43b1-8d50-449ec3c6b4cf',
    '67cdded4-c49f-4fc3-b7e0-52aa2038db91',
    '24a981c1-6ea6-4eab-9225-a5fe3da64477',
    'a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6'
);
