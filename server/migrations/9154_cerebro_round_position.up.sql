-- FIR-3646: rounds carry a user-controlled order so the inbox block and the
-- Manage rounds panel can be reordered by drag and drop. Existing rounds keep
-- their current (created_at) order.
ALTER TABLE cerebro_round ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0;

WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY workspace_id, owner_id ORDER BY created_at, id) - 1 AS rank
    FROM cerebro_round
)
UPDATE cerebro_round r SET position = ranked.rank FROM ranked WHERE ranked.id = r.id;

CREATE INDEX IF NOT EXISTS cerebro_round_owner_position_idx ON cerebro_round(workspace_id, owner_id, position);
