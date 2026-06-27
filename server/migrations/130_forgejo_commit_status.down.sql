DROP TABLE IF EXISTS forgejo_commit_status;
ALTER TABLE forgejo_pull_request DROP COLUMN IF EXISTS head_sha;
