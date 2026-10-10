ALTER TABLE IF EXISTS vcs_connection
    DROP CONSTRAINT IF EXISTS vcs_connection_provider_check;
ALTER TABLE IF EXISTS vcs_connection
    ADD CONSTRAINT vcs_connection_provider_check
    CHECK (provider IN ('forgejo', 'gitea', 'gitlab', 'gongfeng'));

ALTER TABLE IF EXISTS vcs_pull_request
    DROP CONSTRAINT IF EXISTS vcs_pull_request_provider_check;
ALTER TABLE IF EXISTS vcs_pull_request
    ADD CONSTRAINT vcs_pull_request_provider_check
    CHECK (provider IN ('forgejo', 'gitea', 'gitlab', 'gongfeng'));
