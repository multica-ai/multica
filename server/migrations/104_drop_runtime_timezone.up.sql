-- Drop agent_runtime.timezone (Operational tz). The timezone architecture
-- collapsed from 3 layers to 2: Scheduling (autopilot_trigger.timezone) and
-- Viewing (user.timezone). The runtime's physical tz had exactly one
-- consumer -- the hour-of-day heatmap -- which now renders in the viewer's
-- tz like every other report. See docs/timezone-architecture-rfc.md.
ALTER TABLE agent_runtime DROP COLUMN timezone;
