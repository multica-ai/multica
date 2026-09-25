DROP TRIGGER IF EXISTS trg_clear_runtime_mcp_overlay ON agent_task_queue;
DROP FUNCTION IF EXISTS clear_runtime_mcp_overlay_on_terminal_state();
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS runtime_mcp_overlay;
