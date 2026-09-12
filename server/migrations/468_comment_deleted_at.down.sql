-- Dropping the column turns every tombstone back into a live, empty comment
-- that old code would let anyone edit or delete — and deleting it would then
-- cascade to the replies it was kept for. Refuse while tombstones exist. To
-- roll back, roll back the application only and keep this column: older
-- builds ignore it.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM comment WHERE deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'comment tombstones exist; roll back the application and keep comment.deleted_at instead of running this down migration';
    END IF;
END
$$;

ALTER TABLE comment DROP COLUMN IF EXISTS deleted_at;
