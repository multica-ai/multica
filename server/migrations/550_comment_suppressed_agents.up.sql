-- The agents an author chose not to start for this comment, so completion
-- replay honors that choice too instead of waking them once their run ends.
--
-- Nullable with no default, so this is a metadata-only change. The runner
-- sends this file as one implicit transaction: bound lock acquisition and
-- execution so it fails fast and retries on the next run rather than parking a
-- pending ACCESS EXCLUSIVE lock in front of every comment query.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE comment ADD COLUMN IF NOT EXISTS suppressed_agent_ids UUID[] NULL;
