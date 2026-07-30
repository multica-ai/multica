-- FIR-3589: a role can be held by several people or agents at once, and one
-- person can hold several roles. The single owner_type/owner_id pair on the
-- seat could only ever express the first half, so ownership moves to its own
-- ordered join table and the columns on the seat go away.

CREATE TABLE IF NOT EXISTS cerebro_org_chart_seat_owner (
    seat_id UUID NOT NULL REFERENCES cerebro_org_chart_seat(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('member', 'agent')),
    owner_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (seat_id, owner_type, owner_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_org_chart_seat_owner_seat_position
    ON cerebro_org_chart_seat_owner (seat_id, position, created_at);

CREATE INDEX IF NOT EXISTS idx_cerebro_org_chart_seat_owner_workspace
    ON cerebro_org_chart_seat_owner (workspace_id);

-- The backfill reads columns this same migration then drops, so a re-run would
-- fail to even parse it. Guarding on the column's existence keeps the migration
-- repeat-safe, which deploy recovery relies on.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'cerebro_org_chart_seat' AND column_name = 'owner_type'
    ) THEN
        EXECUTE $backfill$
            INSERT INTO cerebro_org_chart_seat_owner (seat_id, workspace_id, owner_type, owner_id, position)
            SELECT id, workspace_id, owner_type, owner_id, 0
            FROM cerebro_org_chart_seat
            WHERE owner_type IS NOT NULL AND owner_id IS NOT NULL
            ON CONFLICT DO NOTHING
        $backfill$;
    END IF;
END $$;

ALTER TABLE cerebro_org_chart_seat
    DROP COLUMN IF EXISTS owner_type,
    DROP COLUMN IF EXISTS owner_id;
