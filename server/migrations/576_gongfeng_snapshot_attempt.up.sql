ALTER TABLE IF EXISTS vcs_pull_request ADD COLUMN IF NOT EXISTS snapshot_attempted_at TIMESTAMPTZ;
