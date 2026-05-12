-- Cerebro workflow engine, phase 2 (JEH-1103, PR 1 of 3).
--
-- Schema-level additions on top of 9020:
--
--   * editor_mode  TEXT NOT NULL DEFAULT 'form'   — which editor opens this
--                                                   workflow ('form' or
--                                                   'canvas'). Existing rows
--                                                   stay on 'form' so phase-1
--                                                   workflows keep behaving
--                                                   exactly as before.
--   * editor_layout JSONB                          — node-canvas positions
--                                                   (xyflow). Only set when
--                                                   editor_mode='canvas'.
--                                                   Purely visual; the
--                                                   trigger/condition/action
--                                                   JSON columns remain the
--                                                   single source of truth.
--
-- The action CHECK constraint is extended to allow the two new phase-2
-- actions (`run_skill`, `comment_on_issue`). `create_sub_issue` was already
-- whitelisted; phase 2 only adds new action_config fields, no new enum value.
--
-- All idempotent — re-running this migration on an already-migrated DB is a
-- no-op.

ALTER TABLE cerebro_workflow
    ADD COLUMN IF NOT EXISTS editor_mode TEXT NOT NULL DEFAULT 'form',
    ADD COLUMN IF NOT EXISTS editor_layout JSONB;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cerebro_workflow_editor_mode_known'
    ) THEN
        ALTER TABLE cerebro_workflow
            ADD CONSTRAINT cerebro_workflow_editor_mode_known
            CHECK (editor_mode IN ('form', 'canvas'));
    END IF;
END$$;

-- Replace the action-type CHECK constraint with the phase-2 expanded list.
-- DROP + ADD is the only portable way to widen a CHECK; doing it in one
-- statement keeps the table validation gap as small as possible.
ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_action_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder',
            'run_skill',
            'comment_on_issue'
        )
    );
