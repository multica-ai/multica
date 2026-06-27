ALTER TABLE issue_vcs_pull_request RENAME TO issue_forgejo_pull_request;
ALTER TABLE vcs_commit_status RENAME TO forgejo_commit_status;
ALTER TABLE vcs_pull_request DROP COLUMN IF EXISTS provider;
ALTER TABLE vcs_pull_request RENAME TO forgejo_pull_request;
ALTER TABLE vcs_connection DROP COLUMN IF EXISTS provider;
ALTER TABLE vcs_connection RENAME TO forgejo_connection;
