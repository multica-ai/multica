ALTER TABLE cerebro_org_chart_seat
    ADD COLUMN IF NOT EXISTS owner_type TEXT,
    ADD COLUMN IF NOT EXISTS owner_id UUID;

ALTER TABLE cerebro_org_chart_seat
    DROP CONSTRAINT IF EXISTS cerebro_org_chart_seat_owner_type_check,
    ADD CONSTRAINT cerebro_org_chart_seat_owner_type_check
        CHECK (owner_type IS NULL OR owner_type IN ('member', 'agent'));

-- The restore reads the table this same migration then drops, so a retried
-- rollback would not even parse it without the guard.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'cerebro_org_chart_seat_owner') THEN
        EXECUTE $restore$
            UPDATE cerebro_org_chart_seat seat
            SET owner_type = first_owner.owner_type,
                owner_id = first_owner.owner_id
            FROM (
                SELECT DISTINCT ON (seat_id) seat_id, owner_type, owner_id
                FROM cerebro_org_chart_seat_owner
                ORDER BY seat_id, position ASC, created_at ASC
            ) AS first_owner
            WHERE seat.id = first_owner.seat_id
        $restore$;
    END IF;
END $$;

DROP TABLE IF EXISTS cerebro_org_chart_seat_owner;
