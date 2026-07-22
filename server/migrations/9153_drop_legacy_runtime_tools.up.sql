-- FIR-3403: preserve the former runtime inventory choices in canonical tool
-- policy, then irreversibly remove the duplicate runtime-tool grant store.

-- cerebro_runtime_tool is UNIQUE on (runtime_id, tool_name, mcp_server_name), so
-- the same (runtime_id, tool_name) can appear under several mcp_server_name with
-- differing `enabled`. The runtime-layer policy key ignores the server, so those
-- rows collapse onto one cerebro_tool_policy row; without a GROUP BY the
-- ON CONFLICT winner (allow vs deny) was row-order dependent. Collapse
-- deterministically and tighten-only: 'allow' only when EVERY legacy row for the
-- (runtime, tool) enabled it (bool_and); any disabled row makes it 'deny'.
INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, resource_pattern, updated_by)
SELECT ar.workspace_id, rt.tool_name, 'runtime', rt.runtime_id,
       CASE WHEN bool_and(rt.enabled) THEN 'allow' ELSE 'deny' END, '', NULL
FROM cerebro_runtime_tool rt
JOIN agent_runtime ar ON ar.id = rt.runtime_id
GROUP BY ar.workspace_id, rt.runtime_id, rt.tool_name
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

-- Group and user grants were scoped to ONE runtime in the legacy store (both
-- tables key on runtime_id). Preserve that scope: aggregate the granted
-- runtimes per (subject, tool) into an arg_allowlist condition on runtime_id,
-- so the migrated Allow applies exactly where the legacy grant did. The
-- resolver threads the resolving runtime into RequestContext.ArgValues
-- (toolpolicy.Store.loadInput), and ConditionedSetting closes an unmet
-- conditional Allow to Deny — a grant scoped to runtime A can never widen
-- into a workspace-wide grant. updated_by collapses across granters, so the
-- migrated row keeps the deterministically-first one for provenance.
INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, resource_pattern, conditions, updated_by)
SELECT ar.workspace_id, grant_row.tool_name, 'group', grant_row.group_id, 'allow', '',
       jsonb_build_object('arg_allowlist', jsonb_build_array(jsonb_build_object(
           'arg', 'runtime_id',
           'values', to_jsonb(array_agg(DISTINCT grant_row.runtime_id::text))))),
       min(grant_row.granted_by::text)::uuid
FROM cerebro_runtime_tool_group_grant grant_row
JOIN agent_runtime ar ON ar.id = grant_row.runtime_id
GROUP BY ar.workspace_id, grant_row.group_id, grant_row.tool_name
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, resource_pattern, conditions, updated_by)
SELECT ar.workspace_id, grant_row.tool_name, 'user', grant_row.user_id, 'allow', '',
       jsonb_build_object('arg_allowlist', jsonb_build_array(jsonb_build_object(
           'arg', 'runtime_id',
           'values', to_jsonb(array_agg(DISTINCT grant_row.runtime_id::text))))),
       min(grant_row.granted_by::text)::uuid
FROM cerebro_runtime_tool_user_grant grant_row
JOIN agent_runtime ar ON ar.id = grant_row.runtime_id
GROUP BY ar.workspace_id, grant_row.user_id, grant_row.tool_name
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting, resource_pattern, updated_by)
SELECT a.workspace_id, override_row.tool_name, 'agent', override_row.agent_id,
       CASE WHEN override_row.enabled THEN 'allow' ELSE 'deny' END, '', override_row.updated_by
FROM cerebro_agent_runtime_tool_override override_row
JOIN agent a ON a.id = override_row.agent_id
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

DROP TABLE IF EXISTS cerebro_agent_runtime_tool_override;
DROP TABLE IF EXISTS cerebro_runtime_tool_user_grant;
DROP TABLE IF EXISTS cerebro_runtime_tool_group_grant;
DROP TABLE IF EXISTS cerebro_runtime_tool;
