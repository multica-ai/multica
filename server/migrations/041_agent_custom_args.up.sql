-- Add custom_args column to agent table for user-configurable CLI arguments
-- that get appended to the agent subprocess command at launch time.
-- Stored as JSONB array of strings (e.g. ["--model", "o3", "--max-turns", "50"]).
ALTER TABLE agent ADD COLUMN custom_args JSONB NOT NULL DEFAULT '[]';
