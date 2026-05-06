ALTER TABLE project
  ADD COLUMN settings JSONB NOT NULL DEFAULT '{}'::jsonb;
