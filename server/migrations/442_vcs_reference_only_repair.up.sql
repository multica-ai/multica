-- Repair installations where issue_vcs_pull_request was created by an early
-- VCS integration build before reference_only was present. Migration 216 uses
-- CREATE TABLE IF NOT EXISTS, so rerunning it cannot reconcile that partial
-- schema once its version is recorded.
ALTER TABLE issue_vcs_pull_request
    ADD COLUMN IF NOT EXISTS reference_only BOOLEAN NOT NULL DEFAULT FALSE;
