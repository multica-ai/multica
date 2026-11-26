-- User notes are configuration metadata, separate from process env values.
ALTER TABLE agent ADD COLUMN custom_env_notes JSONB NOT NULL DEFAULT '{}';
