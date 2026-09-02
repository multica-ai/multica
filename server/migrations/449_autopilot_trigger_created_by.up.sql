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
-- BACKFILL is best-effort and knowingly imprecise. For a trigger nobody has
-- edited, published_by IS the creator and this recovers the right human. For an
-- ALREADY-EDITED trigger it freezes the last recoverable EDITOR as the immutable
-- creator — not necessarily the historical creator, which the schema never
-- recorded and which is therefore unrecoverable. That is accepted (MUL-6951, Elon
-- review): it is no wider than the authority those runs carry today, and the
-- alternative is stopping every existing autopilot.
--
-- A trigger with no published_by at all (predating migration 186) stays NULL.
-- Dispatch then fails closed rather than guessing a principal, and there is
-- deliberately NO recovery path — re-saving such a trigger re-stamps published_by,
-- not created_by, so it stays unresolvable (Bohan: leave them empty). Its runs are
-- refused with a recorded failure_reason.
--
-- The table holds one row per configured trigger (small, bounded by autopilot
-- count), so this runs as a single statement rather than a batched backfill.
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
