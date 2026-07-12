-- Revert to a single workspace per installation.
--
-- NOTE: this will fail if any installation_id is bound to more than one
-- workspace (which the widened schema permits) — reverting a widened
-- uniqueness constraint requires the data to already satisfy the narrower one.
DROP INDEX IF EXISTS idx_github_installation_installation_id;

ALTER TABLE github_installation
    DROP CONSTRAINT github_installation_workspace_id_installation_id_key;

ALTER TABLE github_installation
    ADD CONSTRAINT github_installation_installation_id_key
    UNIQUE (installation_id);
