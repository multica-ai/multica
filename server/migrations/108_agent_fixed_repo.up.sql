ALTER TABLE agent ADD COLUMN fixed_repo_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE agent ADD COLUMN fixed_repo_paths text[] NOT NULL DEFAULT '{}';
ALTER TABLE agent ADD COLUMN vcs_type text NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN cleanup_script text NOT NULL DEFAULT '';
