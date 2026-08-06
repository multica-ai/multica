-- 9165_cerebro_purge_advisory_platform_rows: clean policy slate for the
-- platform capabilities moving from "Managed externally" (advisory) to real
-- tool-policy enforcement (FIR-4220). While these keys were advisory, the
-- Settings screen rejected writes but historical/legacy rows may still exist
-- from before the rejection landed. Once enforcement flips on, a stale stored
-- Deny/Ask would silently change live behaviour on deploy — so the slate is
-- wiped first. Authored choices made after this migration are intentional and
-- enforced.
DELETE FROM cerebro_tool_policy
WHERE tool_key IN (
    'rerun_issue',
    'trigger_autopilot',
    'autopilot_scope',
    'schedule_agent_wakeup',
    'use_other_runtime',
    'manage_project_access',
    'read_issues',
    'read_projects'
);
