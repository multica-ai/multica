ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_fixed_repo_vcs_type_check;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_fixed_repo_paths_array_check;

ALTER TABLE agent
    DROP COLUMN IF EXISTS fixed_repo_cleanup_script,
    DROP COLUMN IF EXISTS fixed_repo_vcs_type,
    DROP COLUMN IF EXISTS fixed_repo_paths,
    DROP COLUMN IF EXISTS fixed_repo_enabled;
