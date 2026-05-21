ALTER TABLE project
  ADD COLUMN workdir_policy TEXT NOT NULL DEFAULT 'none',
  ADD COLUMN canonical_workdir TEXT,
  ADD CONSTRAINT project_workdir_policy_check
    CHECK (workdir_policy IN ('none', 'advisory'));
