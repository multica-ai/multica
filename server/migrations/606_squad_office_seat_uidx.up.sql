-- One live squad per seat. Archived squads release their seat for reuse.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_office_seat_uidx ON squad (floor_id, office_slot) WHERE archived_at IS NULL AND floor_id IS NOT NULL;
