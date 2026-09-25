DROP TABLE IF EXISTS plugin_remote_mcp_oauth_state;

DELETE FROM plugin_remote_mcp_secret
WHERE id IN (SELECT secret_ref FROM plugin_installation_config WHERE auth_type = 'oauth');

DELETE FROM plugin_installation_config WHERE auth_type = 'oauth';

ALTER TABLE plugin_installation_config
    DROP CONSTRAINT IF EXISTS plugin_installation_config_discovered_digest_check,
    DROP COLUMN IF EXISTS discovered_schema_digest,
    DROP COLUMN IF EXISTS discovered_tools;

ALTER TABLE plugin_installation_config
    DROP CONSTRAINT plugin_installation_config_auth_type_check;

ALTER TABLE plugin_installation_config
    ADD CONSTRAINT plugin_installation_config_auth_type_check
    CHECK (auth_type IN ('none', 'bearer', 'header'));
