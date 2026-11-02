-- MUL-7267: give legacy autopilot triggers the dispatch principal migration 449
-- could not recover.
--
-- Migration 449 backfilled autopilot_trigger.created_by from published_by and
-- left the row NULL when there was no published_by either: every trigger created
-- before migration 189 that nobody substantively edited since. A schedule/webhook
-- dispatch acts as created_by and fails closed without one (MUL-6951), so since
-- that upgrade each such trigger has produced only skipped runs — the webhook
-- delivery still reads "dispatched" and the schedule still advances, but nothing
-- executes (#8284).
--
-- This fills those rows from the AUTOPILOT's creator, reversing 449's "leave them
-- empty" choice. The value is a best-effort INFERENCE frozen here, not a record of
-- who created the trigger: before migration 189 an autopilot and its triggers were
-- normally created together, in one dialog, by one member, so for most rows it is
-- the historical creator — but a trigger another member added later through the
-- edit dialog gets the autopilot's creator instead.
--
-- This IS a widening, accepted as the same compatibility tradeoff 449 made. The
-- member chosen is the one automatic dispatch was ADMITTED as before MUL-6951
-- (canCreatorInvokeAgent), so admission decides as it did before that upgrade.
-- Execution does not: a pre-MUL-6951 automatic run carried no originator, only
-- narrowly-scoped borrow paths, whereas a run of a filled trigger acts as the
-- autopilot's creator with that member's own rights, and every run delegated from
-- it inherits that principal. The alternative, NULL, has no in-product recovery
-- that keeps the trigger: an edit re-stamps published_by only, so the user must
-- delete and re-create it, which also changes a webhook's URL for every external
-- sender.
--
-- Only rows without a member principal are touched. An existing member is never
-- rewritten, so an edit still cannot move who a trigger acts as. The autopilot
-- creator must be a member of the autopilot's workspace now; a departed creator is
-- not written in to regain rights if they are re-invited later, and such rows keep
-- failing closed. Dispatch still re-validates membership and invoke access on
-- every run.
--
-- Rollback cannot take this back: from the next schedule or webhook fire the
-- trigger runs as the filled member, and neither an application rollback nor the
-- down migration clears the value.
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

-- 449's comments describe created_by as the creator, written once at creation.
-- That now holds only for triggers created since MUL-6951, and the text is
-- generated into pkg/db/generated/models.go, so correct it here.
COMMENT ON COLUMN autopilot_trigger.created_by_type IS
    'Actor type of created_by_id: member | agent. Only ''member'' yields a run principal. NULL only for a legacy trigger that neither backfill (migrations 449, 467) could fill.';

COMMENT ON COLUMN autopilot_trigger.created_by_id IS
    'The member a schedule/webhook run fires AS: dispatch admission, the task''s originator/accountable, and every delegated run all resolve to this one human (MUL-6951). For a trigger created since MUL-6951 it is the creator, written at creation. For a legacy trigger it is a best-effort principal inferred once by backfill and frozen (the last publisher, migration 449, else the autopilot''s creator, migration 467), not proof of who created it. Ordinary edits never rewrite it, so editing the trigger cannot re-authorize its runs as the editor. NULL means no principal and the dispatch fails closed. No FK; workspace membership is re-validated on every dispatch.';
