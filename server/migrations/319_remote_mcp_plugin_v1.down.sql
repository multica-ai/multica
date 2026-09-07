DROP TRIGGER IF EXISTS trg_plugin_installation_config_append_only ON plugin_installation_config;
DROP TABLE IF EXISTS plugin_installation_config;
DROP TABLE IF EXISTS plugin_remote_mcp_secret;

ALTER TABLE plugin_contribution
    DROP CONSTRAINT IF EXISTS plugin_contribution_type_check;

ALTER TABLE plugin_contribution
    ADD CONSTRAINT plugin_contribution_type_check
    CHECK (type IN ('agent.skill.v1'));
