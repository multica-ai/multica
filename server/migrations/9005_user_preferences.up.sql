-- Fork-specific: per-user preferences for fork-only features.
-- Stored as JSONB to keep the column extensible without further migrations
-- when new toggles are added (e.g. composer keybinding, theme overrides).
ALTER TABLE "user"
    ADD COLUMN preferences JSONB NOT NULL DEFAULT '{}'::jsonb;
