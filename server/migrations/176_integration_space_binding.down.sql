DROP TRIGGER IF EXISTS trg_channel_integration_binding_cleanup ON channel_installation;
DROP FUNCTION IF EXISTS cleanup_channel_integration_space_binding();
DROP TRIGGER IF EXISTS trg_github_integration_binding_cleanup ON github_installation;
DROP FUNCTION IF EXISTS cleanup_integration_space_binding();
DROP TABLE IF EXISTS integration_space_binding;
