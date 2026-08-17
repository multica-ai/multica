-- Remove the resource-scoped (agent/skill) labels before 171.down rebuilds
-- the pre-162 workspace-wide unique name index, which cannot coexist with
-- rows that share (workspace_id, LOWER(name)) across resource types. This
-- runs first in the down chain (high->low), and resource_type still exists
-- here because 162.down (which drops it) runs later.
DELETE FROM issue_label WHERE resource_type <> 'issue';
