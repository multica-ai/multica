-- Bind a workflow node run to the runtime/device/CSC session executing it, so
-- Cloud Web can locate and attach to the live session for real-time
-- collaboration (Design Two). device_id and session_id are free-text
-- identifiers owned by cs-cloud / CSC; runtime_id references the Multica runtime.
ALTER TABLE multica_workflow_node_run
    ADD COLUMN runtime_id UUID REFERENCES multica_agent_runtime(id) ON DELETE SET NULL,
    ADD COLUMN device_id  TEXT,
    ADD COLUMN session_id TEXT;

CREATE INDEX idx_workflow_node_run_session_id ON multica_workflow_node_run(session_id);
