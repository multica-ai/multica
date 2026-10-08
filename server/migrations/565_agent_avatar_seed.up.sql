-- Generated agent avatars (MAKE-291): a persisted random seed that is the
-- agent's stable visual identity. The API projects the display value into
-- avatar_url responses (image > gen:<seed> > legacy emoji marker > fallback);
-- this migration only adds and backfills the seed. Stored avatar_url values
-- are deliberately untouched so hand-picked emoji stay as legacy/fallback
-- identity.
--
-- Every statement is idempotent so the backfill is safe to re-run:
--  * IF NOT EXISTS skips a column that already exists,
--  * the UPDATE only touches rows whose seed is still NULL (never overwrites
--    an existing seed, never rewrites avatar_url),
--  * SET NOT NULL / SET DEFAULT are no-ops when already in that state.
--
-- The DEFAULT is what seeds newly created agents: every insert path
-- (public CreateAgent, builder/system carriers, fixtures) omits avatar_seed
-- and gets a cryptographically random UUID text at creation time.

ALTER TABLE agent ADD COLUMN IF NOT EXISTS avatar_seed TEXT;

UPDATE agent
SET avatar_seed = gen_random_uuid()::text
WHERE avatar_seed IS NULL;

ALTER TABLE agent ALTER COLUMN avatar_seed SET NOT NULL;
ALTER TABLE agent ALTER COLUMN avatar_seed SET DEFAULT gen_random_uuid()::text;
