-- Generalize the Forgejo-only tables into a provider-tagged set shared by all
-- token-based Git forges (Forgejo, Gitea, GitLab). GitHub keeps its own tables
-- (App installations + check suites are too different). The forgejo_* tables
-- were introduced in 128-130 and carry no production data that predates this,
-- so a rename + provider column is safe.
--
-- vcs_commit_status.state now stores the NORMALIZED vocabulary
-- (passed | failed | pending) produced by the provider adapters, rather than a
-- raw forge-specific status, so the aggregation query is provider-independent.

ALTER TABLE forgejo_connection RENAME TO vcs_connection;
ALTER TABLE vcs_connection
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'forgejo'
        CHECK (provider IN ('forgejo', 'gitea', 'gitlab'));

ALTER TABLE forgejo_pull_request RENAME TO vcs_pull_request;
ALTER TABLE vcs_pull_request
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'forgejo'
        CHECK (provider IN ('forgejo', 'gitea', 'gitlab'));

ALTER TABLE forgejo_commit_status RENAME TO vcs_commit_status;

ALTER TABLE issue_forgejo_pull_request RENAME TO issue_vcs_pull_request;
