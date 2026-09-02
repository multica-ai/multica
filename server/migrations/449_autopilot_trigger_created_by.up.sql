-- MUL-6951: give autopilot_trigger an IMMUTABLE creator, distinct from
-- published_by.
--
-- Since MUL-6951 a schedule/webhook run acts as a human, so the column naming
-- that human is an authorization input, not an audit label. published_by cannot
-- serve that role: it means "who is currently responsible for this trigger's
-- effective config" and TRANSFERS to whoever last substantively edits the
-- trigger (MUL-4302). Using it would mean a collaborator adjusting a cron
-- expression silently hands the automation their own invoke rights — a change
-- nothing in the UI expresses. Bohan's ruling on the MUL-6951 thread is that the
-- run always acts as the trigger's CREATOR.
--
-- created_by_* is written once at creation and never rewritten by the edit
-- paths; published_by keeps its existing audit meaning unchanged.
--
-- BACKFILL is best-effort and deliberately incomplete: for a trigger nobody has
-- edited, published_by IS the creator, so it recovers the right human. For an
-- already-edited trigger it recovers the last editor — the closest recoverable
-- human, and no worse than the behaviour shipping today. A trigger with no
-- published_by at all stays NULL, and dispatch fails closed rather than guessing
-- a principal. The table holds one row per configured trigger (small, bounded by
-- autopilot count), so this runs as a single statement rather than a batched
-- backfill.
--
-- No foreign key by house rule; the referenced member is re-validated in
-- application code on every dispatch, which is what actually matters here since
-- a member can be removed from the workspace long after the row is written.
ALTER TABLE autopilot_trigger ADD COLUMN IF NOT EXISTS created_by_type TEXT;
ALTER TABLE autopilot_trigger ADD COLUMN IF NOT EXISTS created_by_id UUID;

UPDATE autopilot_trigger
SET created_by_type = published_by_type,
    created_by_id = published_by_id
WHERE created_by_id IS NULL
  AND published_by_id IS NOT NULL
  AND published_by_type IS NOT NULL;
