-- CEREBRO-PATCH(workspace-copy): revert workspace-copy schema.
DROP TABLE IF EXISTS cerebro_workspace_copy_map;
-- Note: agent.runtime_id is intentionally left nullable on down — re-adding
-- NOT NULL would fail if any parked (runtime-less) agents exist. Re-pair first.
