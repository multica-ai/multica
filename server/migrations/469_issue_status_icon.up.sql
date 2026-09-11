-- Presentation only. Empty keeps the category default for existing statuses
-- and clients that do not send an icon. No issue or workflow data is rewritten.
ALTER TABLE issue_status ADD COLUMN IF NOT EXISTS icon text NOT NULL DEFAULT '';
