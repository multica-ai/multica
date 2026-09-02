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
-- recorded and which is therefore unrecoverable.
--
-- This IS a widening, and is accepted as a compatibility tradeoff (MUL-6951, Elon
-- review). Before MUL-6951 an automatic run carried no originator at all, only a
-- set of narrowly-scoped borrow paths; after it, a backfilled editor becomes the
-- run's full originator and it acts with that member's own rights. The
-- alternative — backfilling nothing — stops every existing autopilot, which is
-- why this is preferred over precision here.
--
-- A trigger with no published_by at all (predating migration 189, which added
-- published_by_* to this table) stays NULL.
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

-- Migration 189 documented published_by_* as deciding the accountable human of the
-- runs a trigger fires. That is no longer true and the text is generated into
-- pkg/db/generated/models.go, so it would keep asserting the old authorization
-- model to every reader. 189 is already released and must not be edited; override
-- the comments here instead.
COMMENT ON COLUMN autopilot_trigger.published_by_type IS
    'Actor type of the trigger''s current responsible publisher: member | agent. Set to the creator at creation and re-stamped to the editor on any substantive edit governing this trigger. CONFIG audit only — since MUL-6951 it decides nothing about the runs this trigger fires. NULL on triggers predating MUL-4302.';

COMMENT ON COLUMN autopilot_trigger.published_by_id IS
    'The member/agent currently responsible for this trigger''s effective config (creator, then last substantive editor). CONFIG audit only: since MUL-6951 the runs this trigger fires act as, and are accountable to, created_by_id instead, so an edit recorded here never moves a run''s authority. No FK, app-layer integrity (MUL-4302).';

COMMENT ON COLUMN autopilot_trigger.created_by_type IS
    'Actor type of the trigger''s immutable creator: member | agent. Only ''member'' yields a run principal. NULL for triggers created before MUL-6951 that had no published_by to backfill from.';

COMMENT ON COLUMN autopilot_trigger.created_by_id IS
    'The member a schedule/webhook run fires AS: dispatch admission, the task''s originator/accountable, and every delegated run all resolve to this one human (MUL-6951). Written once at creation and never re-stamped, so editing the trigger cannot re-authorize its runs as the editor. NULL means no provable principal and the dispatch fails closed. No FK; workspace membership is re-validated on every dispatch.';
