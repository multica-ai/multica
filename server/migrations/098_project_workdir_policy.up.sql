ALTER TABLE project
ADD COLUMN workdir_policy text NOT NULL DEFAULT 'none',
ADD COLUMN canonical_workdir text;

ALTER TABLE project
ADD CONSTRAINT project_workdir_policy_check CHECK (workdir_policy IN ('none', 'advisory'));
