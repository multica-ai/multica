-- The Secretary's digest source (spec 4.8): auto, squads or floors. A plain
-- column rather than a key in workspace.settings, because clients replace
-- settings as a whole object (see 630) and a stale client would silently reset
-- it. auto reads floor briefs above 30 active squads.
--
-- The CHECK is added NOT VALID and validated after, as in 782.
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS digest_mode TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE workspace DROP CONSTRAINT IF EXISTS workspace_digest_mode_check;
ALTER TABLE workspace ADD CONSTRAINT workspace_digest_mode_check
    CHECK (digest_mode IN ('auto', 'squads', 'floors')) NOT VALID;
ALTER TABLE workspace VALIDATE CONSTRAINT workspace_digest_mode_check;
