ALTER TABLE code_graph_repository DROP COLUMN IF EXISTS credential_id;
ALTER TABLE code_graph_repository DROP COLUMN IF EXISTS auth_mode;
DROP TABLE IF EXISTS code_graph_git_credential;
