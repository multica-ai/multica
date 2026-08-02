-- Allow 'chained' as an autopilot_run source so chain-triggered dispatches
-- are distinguishable from schedule / manual / webhook / api. The CHECK was
-- originally unnamed in migration 042, so find it by column.
DO $$
DECLARE cname text;
BEGIN
    SELECT con.conname INTO cname
    FROM pg_constraint con
      JOIN pg_class rel ON rel.oid = con.conrelid
      JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
    WHERE nsp.nspname = 'public'
      AND rel.relname = 'autopilot_run'
      AND con.contype = 'c'
      AND pg_get_constraintdef(con.oid) ILIKE '%source%';
    IF cname IS NOT NULL THEN
        EXECUTE format('ALTER TABLE autopilot_run DROP CONSTRAINT %I', cname);
    END IF;
END $$;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api', 'chained'));

-- Successor edges for cross-autopilot chain triggering (WS-768 / Stage 4).
-- When an autopilot run reaches a terminal state that matches on_status, the
-- dispatch layer looks up successor rows for that autopilot and fires each
-- successor via the same DispatchAutopilot path used by schedule / webhook /
-- manual triggers. No FK on successor_autopilot_id — both autopilots must
-- live in the same workspace, enforced at the API layer (mirrors the
-- autopilot_subscriber pattern: app-level checks, cascade on predecessor
-- delete via the autopilot_id FK).

CREATE TABLE autopilot_successor (
    autopilot_id         UUID        NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    successor_autopilot_id UUID      NOT NULL,
    workspace_id         UUID        NOT NULL,
    on_status            TEXT        NOT NULL DEFAULT 'completed'
                                     CHECK (on_status IN ('completed', 'failed', 'both')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (autopilot_id, successor_autopilot_id),
    -- Self-loops are meaningless and would waste a dispatch cycle.
    CONSTRAINT chk_autopilot_successor_no_self_loop
        CHECK (autopilot_id <> successor_autopilot_id)
);

CREATE INDEX idx_autopilot_successor_workspace
    ON autopilot_successor (workspace_id);

-- Reverse lookup: "which autopilots chain into this one?" (predecessors).
CREATE INDEX idx_autopilot_successor_reverse
    ON autopilot_successor (successor_autopilot_id);
