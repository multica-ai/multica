ALTER TABLE autopilot
ADD COLUMN manual_options TEXT[] NOT NULL DEFAULT '{}'::text[];
