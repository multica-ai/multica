-- Refuse to erase reservation history even during a rollback.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM support_dispatch_claim) THEN
        RAISE EXCEPTION 'support dispatch claims require separately approved reconciliation before rollback';
    END IF;
END $$;
DROP TABLE support_dispatch_claim;
