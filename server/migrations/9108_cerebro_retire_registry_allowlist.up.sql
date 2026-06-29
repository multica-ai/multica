-- 9108_cerebro_retire_registry_allowlist: retire System A (per-agent
-- allowed_data_sources / allowed_data_sources_all in agent_tool_grant.config_json)
-- and migrate all existing configs into System B (cerebro_tool_policy chain rows).
--
-- FIR-2208. Both systems ran in parallel: System A was the sole enforcing source;
-- System B (FIR-2083) was wired as a tightening-only gate behind a flag. This
-- migration promotes System B to the sole enforcing source by:
--   1. Converting each agent's existing allowlist into the equivalent chain rule.
--   2. Removing the flag check from the gate (code change, not SQL).
--
-- Three cases per agent (resource_pattern='' = capability-wide row):
--   a. allowed_data_sources_all=true  → Allow row, no condition (Base=Allow covers
--      it anyway, but the explicit row survives a future Base change).
--   b. allowed_data_sources=[ids]      → Allow row, ArgAllow condition on
--      data_source_id ∈ ids.  Unmet conditional Allow closes to Deny, so sources
--      outside the list are blocked — no access widened.
--   c. empty / null config             → Deny row, no condition. Preserves the
--      current deny-all-by-default when no sources were ever granted.
--
-- ON CONFLICT DO NOTHING keeps any admin-authored chain rows already in place.

INSERT INTO cerebro_tool_policy (
    workspace_id,
    tool_key,
    layer,
    subject_id,
    setting,
    resource_pattern,
    conditions
)
SELECT
    ag.workspace_id,
    'firtal_registry'             AS tool_key,
    'agent'                       AS layer,
    atg.agent_id                  AS subject_id,
    CASE
        WHEN (atg.config_json ->> 'allowed_data_sources_all')::boolean = true
            THEN 'allow'
        WHEN atg.config_json -> 'allowed_data_sources' IS NOT NULL
             AND jsonb_array_length(atg.config_json -> 'allowed_data_sources') > 0
            THEN 'allow'
        ELSE 'deny'
    END                           AS setting,
    ''                            AS resource_pattern,
    CASE
        WHEN (atg.config_json ->> 'allowed_data_sources_all')::boolean = true
            THEN NULL
        WHEN atg.config_json -> 'allowed_data_sources' IS NOT NULL
             AND jsonb_array_length(atg.config_json -> 'allowed_data_sources') > 0
            THEN jsonb_build_object(
                'arg_allowlist', jsonb_build_array(
                    jsonb_build_object(
                        'arg',    'data_source_id',
                        'values', atg.config_json -> 'allowed_data_sources'
                    )
                )
            )
        ELSE NULL
    END                           AS conditions
FROM agent_tool_grant atg
JOIN agent ag ON ag.id = atg.agent_id
WHERE atg.tool_name = 'firtal_registry'
  AND atg.enabled   = true
  AND atg.config_json IS NOT NULL
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern)
    DO NOTHING;
