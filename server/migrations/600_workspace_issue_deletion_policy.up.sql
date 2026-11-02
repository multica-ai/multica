-- A deletion policy is independent of replaceable client settings snapshots.
ALTER TABLE workspace
  ADD COLUMN IF NOT EXISTS restrict_issue_deletion BOOLEAN NOT NULL DEFAULT FALSE;
