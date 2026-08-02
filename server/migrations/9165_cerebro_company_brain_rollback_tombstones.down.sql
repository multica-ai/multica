DROP TABLE IF EXISTS cerebro_company_brain_tool_alias_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_permission_audit_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_approval_audit_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_approval_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_permission_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_connection_tombstone;
DROP TABLE IF EXISTS cerebro_company_brain_rollback_window;

DROP FUNCTION IF EXISTS cerebro_protect_active_company_brain_rollback();

ALTER TABLE cerebro_tool_policy_audit
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_audit_workspace_identity_unique;

ALTER TABLE cerebro_tool_policy_audit
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_audit_workspace_id_id_unique;

ALTER TABLE cerebro_approval_audit
    DROP CONSTRAINT IF EXISTS cerebro_approval_audit_workspace_approval_id_unique;

ALTER TABLE cerebro_approval_request
    DROP CONSTRAINT IF EXISTS cerebro_approval_request_workspace_identity_unique;

ALTER TABLE cerebro_approval_request
    DROP CONSTRAINT IF EXISTS cerebro_approval_request_workspace_id_id_unique;

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_workspace_identity_unique;

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_workspace_id_id_unique;

ALTER TABLE cerebro_capability_alias
    DROP CONSTRAINT IF EXISTS cerebro_capability_alias_id_capability_unique;

ALTER TABLE workspace_connection
    DROP CONSTRAINT IF EXISTS workspace_connection_workspace_id_id_name_unique;
