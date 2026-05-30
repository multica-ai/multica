ALTER TABLE agent
    ADD COLUMN fixed_repo_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN fixed_repo_paths JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN fixed_repo_vcs_type TEXT NOT NULL DEFAULT 'git',
    ADD COLUMN fixed_repo_cleanup_script TEXT;

ALTER TABLE agent
    ADD CONSTRAINT agent_fixed_repo_paths_array_check
        CHECK (jsonb_typeof(fixed_repo_paths) = 'array'),
    ADD CONSTRAINT agent_fixed_repo_vcs_type_check
        CHECK (fixed_repo_vcs_type IN ('git', 'perforce', 'none', 'custom'));
