-- Generated agent avatars (MAKE-291): a persisted random seed that is the
-- agent's stable generated identity. The API projects the display value into
-- avatar_url responses (image > emoji marker > gen:<seed> > fallback); this
-- migration only adds and backfills the seed. Stored avatar_url values are
-- deliberately untouched, so an existing uploaded image or hand-picked emoji
-- keeps displaying and only agents with neither show the generated avatar.
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
--
-- OPERATIONAL NOTES (read before applying to a large or busy database):
--  * Locking. The runner sends this file as one multi-statement Exec, i.e. a
--    single implicit transaction, so the ACCESS EXCLUSIVE lock that ALTER
--    TABLE takes on `agent` is held until the whole file finishes, including
--    the backfill UPDATE and the SET NOT NULL scan. Reads and writes on
--    `agent` queue behind it for that time.
--  * The backfill UPDATE rewrites every existing agent row (one new tuple
--    version per row; dead tuples are left for autovacuum). Lock duration
--    depends on table size and on concurrent activity. Measured on a local
--    PostgreSQL 17 with synthetic data: roughly 0.06 s at 2k rows, 0.25 s at
--    20k, and about 3 s at 200k. These are not guarantees. Production-sized
--    tables should be measured, and the migration is not risk-free.
--  * Rolling back (down) and re-applying (up) REGENERATES every seed, because
--    the down migration drops the column. An agent that was showing its
--    generated avatar can then show a different generated avatar. Agents with
--    an uploaded image or emoji are unaffected (avatar_url is never touched).
--  * Deployment order. The new backend binary selects avatar_seed in its
--    agent queries, so this migration must be applied BEFORE it starts. An
--    older binary keeps working against a migrated database.

ALTER TABLE agent ADD COLUMN IF NOT EXISTS avatar_seed TEXT;

UPDATE agent
SET avatar_seed = gen_random_uuid()::text
WHERE avatar_seed IS NULL;

ALTER TABLE agent ALTER COLUMN avatar_seed SET NOT NULL;
ALTER TABLE agent ALTER COLUMN avatar_seed SET DEFAULT gen_random_uuid()::text;
