-- MUL-7267: give legacy autopilot triggers the creator migration 449 could not
-- recover.
--
-- Migration 449 backfilled autopilot_trigger.created_by from published_by and
-- left the row NULL when there was no published_by either: every trigger created
-- before migration 189 that nobody substantively edited since. A schedule/webhook
-- dispatch acts as the trigger's creator and fails closed without one (MUL-6951),
-- so since that upgrade each such trigger has produced only skipped runs — the
-- webhook delivery still reads "dispatched" and the schedule still advances, but
-- nothing executes (#8284).
--
-- This fills those rows from the AUTOPILOT's creator, reversing 449's "leave them
-- empty" choice:
--   - it is the principal automatic dispatch was admitted as before MUL-6951
--     (canCreatorInvokeAgent), so no trigger gains an authorization its runs did
--     not already pass before the upgrade;
--   - before migration 189 an autopilot and its triggers were normally created
--     together, in one dialog, by one member, so for most rows this recovers the
--     historical creator rather than guessing one;
--   - NULL has no in-product recovery that keeps the trigger: an edit re-stamps
--     published_by only, so the user must delete and re-create the trigger, which
--     also changes a webhook's URL for every external sender.
--
-- Only rows without a member creator are touched. An existing member creator is
-- never rewritten, so an edit still cannot move who a trigger acts as. The
-- autopilot creator must be a member of the autopilot's workspace now; a departed
-- creator is not written in to regain rights if they are re-invited later, and
-- such rows keep failing closed. Dispatch still re-validates membership and invoke
-- access on every run: this changes no gate, it only supplies the principal those
-- gates evaluate.
--
-- autopilot_trigger holds one row per configured trigger (bounded by autopilot
-- count), so this runs as a single statement, like 449. Re-running it is a no-op.
UPDATE autopilot_trigger t
SET created_by_type = 'member',
    created_by_id = a.created_by_id
FROM autopilot a
WHERE a.id = t.autopilot_id
  AND (t.created_by_id IS NULL OR t.created_by_type IS DISTINCT FROM 'member')
  AND a.created_by_type = 'member'
  AND a.created_by_id IS NOT NULL
  AND EXISTS (
      SELECT 1
      FROM member m
      WHERE m.user_id = a.created_by_id
        AND m.workspace_id = a.workspace_id
  );

COMMENT ON COLUMN autopilot_trigger.created_by_type IS
    'Actor type of the trigger''s immutable creator: member | agent. Only ''member'' yields a run principal. Legacy triggers were backfilled from published_by (migration 449), then from the autopilot''s creator (migration 467); NULL only when neither was a usable member.';
