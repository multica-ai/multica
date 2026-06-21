-- FIR-1771 — "Test as user" feature gate.
--
-- 1) Register the tools:test-as-user capability in every workspace so it shows
--    up as a settable Allow/Ask/Deny row in the Permissions screen and an admin
--    can grant/revoke it per user or group. It is NOT runtime-reported, so it is
--    seeded here as a workspace-level cerebro capability (source 'cerebro').
--
-- 2) Default the feature ON for Jesper only (Jesper's request: "kun default slået
--    til på mig som member i vores permission-lag"). Everyone else stays OFF
--    because the gate resolves with Base = Deny and no admin bypass — an explicit
--    Allow row is required, and only Jesper gets one here. The insert is guarded
--    on Jesper actually being a member of the workspace, so it is a no-op on any
--    database where that user/workspace does not exist.

INSERT INTO cerebro_capability (workspace_id, capability_key, title, category, description, source)
SELECT w.id,
       'tools:test-as-user',
       'Test as user',
       'Platform',
       'Look up another user + agent''s effective tool permission from the profile menu (same answer as the tool-policy explain CLI).',
       'cerebro'
FROM workspace w
ON CONFLICT (workspace_id, capability_key) DO NOTHING;

INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, updated_by)
SELECT m.workspace_id,
       'tools:test-as-user',
       'user',
       m.user_id,
       'allow',
       m.user_id
FROM member m
WHERE m.user_id = 'd7a6fa72-e68d-48ca-86be-2ab4313ecf44'
ON CONFLICT (workspace_id, tool_key, layer, subject_id) DO NOTHING;
