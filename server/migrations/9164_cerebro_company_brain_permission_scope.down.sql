ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_connection_fk,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_scope_complete,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_scope_agent_grant,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_read_sources_valid,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_write_source_valid,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_access_version_valid,
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_lifecycle_known;

ALTER TABLE IF EXISTS cerebro_company_brain_parity_proof
    DROP CONSTRAINT IF EXISTS cerebro_company_brain_parity_proof_permission_fk;

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_parity_identity_unique;

ALTER TABLE cerebro_tool_policy
    DROP COLUMN IF EXISTS company_brain_connection_id,
    DROP COLUMN IF EXISTS company_brain_allowed_read_sources,
    DROP COLUMN IF EXISTS company_brain_write_source,
    DROP COLUMN IF EXISTS company_brain_access_version,
    DROP COLUMN IF EXISTS company_brain_lifecycle_state;

ALTER TABLE cerebro_company_brain_connection
    DROP CONSTRAINT IF EXISTS cerebro_company_brain_connection_workspace_id_id_unique;
