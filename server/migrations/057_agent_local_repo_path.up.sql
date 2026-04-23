-- Adds a per-agent local repository path. When set, the daemon runs the agent
-- directly in this directory instead of creating an isolated workdir.
-- Useful for users who want the agent to operate on an existing local codebase.
ALTER TABLE agent ADD COLUMN local_repo_path TEXT;
