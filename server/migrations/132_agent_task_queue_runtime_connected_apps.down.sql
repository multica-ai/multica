CREATE OR REPLACE FUNCTION clear_runtime_mcp_overlay_on_terminal_state()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status IN ('completed', 'failed', 'cancelled')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND NEW.runtime_mcp_overlay IS NOT NULL THEN
        NEW.runtime_mcp_overlay := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS runtime_connected_apps;
