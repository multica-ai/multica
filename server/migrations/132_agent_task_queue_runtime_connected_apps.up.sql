-- Non-secret task-scoped metadata for MCP overlays. runtime_mcp_overlay holds
-- the actual server URL/headers; this column holds the semantic app mapping
-- the daemon can render into the agent brief, e.g. "Notion via MCP server
-- composio".
ALTER TABLE agent_task_queue
    ADD COLUMN runtime_connected_apps JSONB NULL;

COMMENT ON COLUMN agent_task_queue.runtime_connected_apps IS
    'Non-secret per-task connected app metadata corresponding to runtime_mcp_overlay, used by the daemon brief to tell agents which app capabilities are mounted. Cleared with runtime_mcp_overlay after task completion.';

-- Extend the existing cleanup trigger so completed rows do not retain either
-- the secret overlay or the owner integration footprint.
CREATE OR REPLACE FUNCTION clear_runtime_mcp_overlay_on_terminal_state()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status IN ('completed', 'failed', 'cancelled')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND (NEW.runtime_mcp_overlay IS NOT NULL OR NEW.runtime_connected_apps IS NOT NULL) THEN
        NEW.runtime_mcp_overlay := NULL;
        NEW.runtime_connected_apps := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
