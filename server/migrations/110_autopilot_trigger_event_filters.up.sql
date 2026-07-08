-- Add event_filters to autopilot_trigger so webhook triggers can declare
-- which events/actions they care about. NULL means "accept all" (backward
-- compatible). JSONB shape: [{"event": "workflow_run", "actions": ["completed"]}, …]
ALTER TABLE autopilot_trigger
    ADD COLUMN event_filters JSONB;
